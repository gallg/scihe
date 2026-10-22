# scihe notebook driver: executes code sent by the editor in a persistent
# namespace. Requests arrive on stdin as JSON lines {"id": n, "code": "..."};
# events are written as JSON lines to file descriptor 3.
import ast
import base64
import builtins
import codecs
import io
import json
import linecache
import signal
import os
import re
import select
import subprocess
import sys
import tokenize
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


def _killpg(pgid, sig):
    try:
        os.killpg(pgid, sig)
    except OSError:
        pass


def _sh(cmd):
    """Runs a `!command` line of a cell with the shell, streaming its
    output (stderr included) into the cell like Jupyter. The command gets
    its own process group: an interrupt stops it and nothing else the
    kernel started, and the editor kills it when the kernel goes away."""
    p = subprocess.Popen(cmd, shell=True, stdin=subprocess.DEVNULL,
                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                         start_new_session=True)
    _send({"type": "child", "pgid": p.pid})
    fd = p.stdout.fileno()
    dec = codecs.getincrementaldecoder("utf-8")("replace")
    try:
        while True:
            ready, _, _ = select.select([fd], [], [], 0.05)
            if ready:
                chunk = os.read(fd, 65536)
                if not chunk:
                    break
                sys.stdout.write(dec.decode(chunk))
            elif p.poll() is not None:
                # the command ended; a background job (cmd &) may keep
                # the pipe open, it must not hold the cell
                break
        sys.stdout.write(dec.decode(b"", True))
        p.wait()
    except KeyboardInterrupt:
        _killpg(p.pid, signal.SIGINT)
        try:
            p.wait(timeout=1)
        except subprocess.TimeoutExpired:
            _killpg(p.pid, signal.SIGKILL)
            p.wait()
        raise
    finally:
        p.stdout.close()
        _send({"type": "child", "pgid": 0})


_BANG = re.compile(r"^([ \t]*)!(?!=)(.*)$")
_SH = "__scihe_sh__"


def _shell_lines(code):
    """Turns `!command` lines into calls of the shell helper. Only lines
    that start a statement count (not lines inside strings or brackets,
    nor `!=` continuations); a trailing backslash continues the command.
    Line numbers are kept, so tracebacks still point at the cell."""
    lines = code.split("\n")
    probe = list(lines)
    cmds = {}
    i = 0
    while i < len(lines):
        m = _BANG.match(lines[i])
        if not m:
            i += 1
            continue
        indent, cmd, j = m.group(1), m.group(2), i
        while cmd.endswith("\\") and j + 1 < len(lines):
            j += 1
            cmd = cmd[:-1] + lines[j].strip()
        cmds[i] = (indent, cmd, j)
        probe[i] = indent + _SH
        for k in range(i + 1, j + 1):
            probe[k] = ""
        i = j + 1
    # tokenize with a placeholder name on the candidate lines to see which
    # of them really start a statement
    found = set()
    depth = 0
    prev = None
    try:
        for tok in tokenize.generate_tokens(io.StringIO("\n".join(probe)).readline):
            if tok.type == tokenize.OP:
                if tok.string in ("(", "[", "{"):
                    depth += 1
                elif tok.string in (")", "]", "}"):
                    depth = max(depth - 1, 0)
            elif (tok.type == tokenize.NAME and tok.string == _SH and depth == 0
                  and prev in (None, tokenize.NEWLINE, tokenize.INDENT, tokenize.DEDENT)
                  and tok.start[0] - 1 in cmds):
                found.add(tok.start[0] - 1)
            if tok.type not in (tokenize.NL, tokenize.COMMENT):
                prev = tok.type
    except (tokenize.TokenError, SyntaxError):
        pass  # a real error further on: the lines found before it count
    for i in found:
        indent, cmd, j = cmds[i]
        lines[i] = "%s%s(%r)" % (indent, _SH, cmd)
        for k in range(i + 1, j + 1):
            lines[k] = ""
    return "\n".join(lines)


_ns = {"__name__": "__main__", "__builtins__": builtins, "display": display}
builtins.input = _no_input
builtins.display = display
setattr(builtins, _SH, _sh)
warnings.filterwarnings("ignore", message=".*non-interactive.*")


def _clean_tb(e):
    frames = [f for f in traceback.extract_tb(e.__traceback__) if f.filename != _DRIVER]
    lines = traceback.format_list(frames)
    lines += traceback.format_exception_only(type(e), e)
    return ["Traceback (most recent call last):"] + [l.rstrip("\n") for l in lines]


_sync_fds = []

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
        try:
            tree = ast.parse(code, fname, "exec")
        except SyntaxError:
            # `!command` lines are not Python: only a cell that does not
            # parse is rewritten, so valid code is never touched
            shell = _shell_lines(code)
            if shell == code:
                raise
            try:
                tree = ast.parse(shell, fname, "exec")
            except SyntaxError as e:
                # report the line as the user wrote it
                if e.lineno and e.lineno <= len(code.split("\n")):
                    e.text = code.split("\n")[e.lineno - 1] + "\n"
                raise
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
    # private copies of fd 1/2 for the sync markers: user code that
    # redirects fd 1 (os.dup2) does not get them in its file
    _sync_fds.extend((os.dup(1), os.dup(2)))
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
        # a marker on fd 1 and 2 tells the editor that output written there
        # directly (by child processes) up to now belongs to this request
        for fd in _sync_fds:
            try:
                os.write(fd, b"\x1dSCIHE-SYNC %d\x1d" % _cur[0])
            except OSError:
                pass
        _send({"id": _cur[0], "type": "done", "execution_count": n, "status": status})


main()
