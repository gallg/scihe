# scihe notebook driver: executes code sent by the editor in a persistent
# namespace. Requests arrive on stdin as JSON lines {"id": n, "code": "..."};
# events are written as JSON lines to file descriptor 3.
import ast
import base64
import builtins
import io
import json
import linecache
import signal
import os
import sys
import traceback
import warnings

_proto = os.fdopen(3, "w", buffering=1, encoding="utf-8")
_cur = [0]
_count = [0]
_DRIVER = "<string>"  # the driver runs via `python -c`


def _send(msg):
    _proto.write(json.dumps(msg) + "\n")
    _proto.flush()


class _BinStream(io.RawIOBase):
    """sys.stdout.buffer replacement: decodes bytes and forwards them."""

    def __init__(self, text):
        self._text = text

    def writable(self):
        return True

    def write(self, b):
        self._text.write(bytes(b).decode("utf-8", "replace"))
        return len(b)


class _Stream(io.TextIOBase):
    encoding = "utf-8"
    errors = "replace"

    def __init__(self, name, fd):
        self.name = name
        self._fd = fd
        self.buffer = _BinStream(self)

    def writable(self):
        return True

    def write(self, s):
        if s:
            _send({"id": _cur[0], "type": "stream", "name": self.name, "text": s})
        return len(s)

    def flush(self):
        pass

    def isatty(self):
        return False

    def fileno(self):
        return self._fd


def _mime_bundle(obj):
    data = {"text/plain": repr(obj)}
    for meth, mime in (("_repr_html_", "text/html"), ("_repr_markdown_", "text/markdown"),
                       ("_repr_svg_", "image/svg+xml"), ("_repr_latex_", "text/latex")):
        f = getattr(obj, meth, None)
        if callable(f):
            try:
                v = f()
                if v is not None:
                    data[mime] = v
            except Exception:
                pass
    f = getattr(obj, "_repr_png_", None)
    if callable(f):
        try:
            v = f()
            if isinstance(v, bytes):
                data["image/png"] = base64.b64encode(v).decode()
        except Exception:
            pass
    mpl = sys.modules.get("matplotlib.figure")
    if mpl is not None and isinstance(obj, mpl.Figure):
        try:
            buf = io.BytesIO()
            obj.savefig(buf, format="png", bbox_inches="tight")
            data["image/png"] = base64.b64encode(buf.getvalue()).decode()
        except Exception as e:
            sys.stderr.write("figure could not be rendered: %s\n" % e)
    return data


def display(*objs):
    for o in objs:
        _send({"id": _cur[0], "type": "display_data", "data": _mime_bundle(o)})


def _flush_figures():
    plt = sys.modules.get("matplotlib.pyplot")
    if plt is None:
        return
    try:
        for num in plt.get_fignums():
            fig = plt.figure(num)
            try:
                buf = io.BytesIO()
                fig.savefig(buf, format="png", bbox_inches="tight")
            except Exception as e:
                sys.stderr.write("figure %d could not be rendered: %s\n" % (num, e))
                continue
            _send({"id": _cur[0], "type": "display_data", "data": {
                "image/png": base64.b64encode(buf.getvalue()).decode(),
                "text/plain": "<Figure %dx%d>" % tuple(int(x) for x in fig.get_size_inches() * fig.dpi)}})
    finally:
        # a broken figure must not poison later cells
        plt.close("all")


def _no_input(prompt=""):
    raise RuntimeError("input() is not supported in scihe notebooks")


_ns = {"__name__": "__main__", "__builtins__": builtins, "display": display}
builtins.input = _no_input
builtins.display = display
warnings.filterwarnings("ignore", message=".*non-interactive.*")


def _clean_tb(e):
    frames = [f for f in traceback.extract_tb(e.__traceback__) if f.filename != _DRIVER]
    lines = traceback.format_list(frames)
    lines += traceback.format_exception_only(type(e), e)
    return ["Traceback (most recent call last):"] + [l.rstrip("\n") for l in lines]


# SIGINT only interrupts user code; arriving elsewhere (reading the next
# request, sending results) it is ignored so the protocol stays in sync.
_armed = [False]


def _on_sigint(signum, frame):
    if _armed[0]:
        _armed[0] = False
        raise KeyboardInterrupt


def _run(code):
    _count[0] += 1
    n = _count[0]
    status = "ok"
    fname = "<cell [%d]>" % n
    linecache.cache[fname] = (len(code), None, code.splitlines(True), fname)
    try:
        tree = ast.parse(code, fname, "exec")
        last = None
        if tree.body and isinstance(tree.body[-1], ast.Expr):
            last = ast.Expression(tree.body.pop().value)
        code_obj = compile(tree, fname, "exec")
        last_obj = compile(last, fname, "eval") if last is not None else None
        v = None
        _armed[0] = True
        try:
            exec(code_obj, _ns)
            if last_obj is not None:
                v = eval(last_obj, _ns)
        finally:
            _armed[0] = False
        if last is not None:
            if v is not None:
                _ns["_"] = v
                _send({"id": _cur[0], "type": "execute_result", "execution_count": n,
                       "data": _mime_bundle(v)})
        _armed[0] = True
        try:
            _flush_figures()
        finally:
            _armed[0] = False
    except KeyboardInterrupt as e:
        status = "error"
        _send({"id": _cur[0], "type": "error", "ename": "KeyboardInterrupt", "evalue": "",
               "traceback": ["KeyboardInterrupt"]})
    except SystemExit as e:
        status = "error"
        _send({"id": _cur[0], "type": "error", "ename": "SystemExit", "evalue": str(e.code),
               "traceback": ["SystemExit: %s" % e.code]})
    except BaseException as e:
        status = "error"
        _send({"id": _cur[0], "type": "error", "ename": type(e).__name__, "evalue": str(e),
               "traceback": _clean_tb(e)})
    finally:
        try:
            sys.stdout.flush()
            sys.stderr.flush()
        except Exception:
            pass
    return n, status


def main():
    signal.signal(signal.SIGINT, _on_sigint)
    # requests come on a private copy of fd 0; user code and child
    # processes get /dev/null so they cannot swallow protocol lines
    requests = os.fdopen(os.dup(0), "r", encoding="utf-8")
    devnull = os.open(os.devnull, os.O_RDONLY)
    os.dup2(devnull, 0)
    os.close(devnull)
    sys.stdin = open(os.devnull, "r")
    sys.stdout = _Stream("stdout", 1)
    sys.stderr = _Stream("stderr", 2)
    _send({"type": "ready", "python": sys.version.split()[0], "executable": sys.executable})
    while True:
        try:
            line = requests.readline()
        except KeyboardInterrupt:
            continue
        if not line:
            break
        try:
            req = json.loads(line)
        except ValueError:
            continue
        _cur[0] = req.get("id", 0)
        try:
            n, status = _run(req.get("code", ""))
        except KeyboardInterrupt:
            n, status = _count[0], "error"
        _send({"id": _cur[0], "type": "done", "execution_count": n, "status": status})


main()
