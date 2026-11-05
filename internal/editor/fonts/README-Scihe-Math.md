# Scihe Math

Scihe Math is the math font built into scihe to typeset formulas. It is
**Latin Modern Math 1.959, with its font names changed and nothing else**.
It is distributed under the [GUST Font License](GUST-FONT-LICENSE.txt), not
under scihe's MIT License. The files of this font are listed in its
[manifest](MANIFEST-Scihe-Math.md).

## Original work

- **Font:** Latin Modern Math, version 1.959 (5 IX 2014)
- **Authors:** Bogusław Jackowski, Piotr Strzelczyk and Piotr Pianowski
- **Copyright:** Copyright 2012--2014 for the Latin Modern math extensions
  by B. Jackowski, P. Strzelczyk and P. Pianowski (on behalf of TeX Users
  Groups).
- **License:** This work can be freely used and distributed under the GUST
  Font License (GFL -- see [GUST-FONT-LICENSE.txt](GUST-FONT-LICENSE.txt)),
  which is actually an instance of the LaTeX Project Public License
  (LPPL -- see <http://www.latex-project.org/lppl.txt>).
- **Maintenance:** the work has the maintenance status "maintained"; its
  Current Maintainers are B. Jackowski, P. Strzelczyk and P. Pianowski.

**Get the complete, unmodified original** (the font with its documentation and
test files) from CTAN: <https://ctan.org/pkg/lm-math>. Latin Modern, the
family it belongs to, is at <http://www.gust.org.pl/projects/e-foundry/latin-modern>.

## Changes from the original

This is a derived work in the sense of the LPPL: it modifies the font and
distributes only part of the original package.

1. **Font names.** In `scihe-math.otf` (the original `latinmodern-math.otf`)
   the font names were changed, as clause 1 of the GUST Font License
   requests for derived works:

   | Name | Original | Scihe Math |
   |------|----------|------------|
   | family (name IDs 1, 16; CFF FamilyName) | Latin Modern Math | Scihe Math |
   | PostScript and full name (name IDs 4, 6; CFF FontName, FullName) | LatinModernMath-Regular | SciheMath-Regular |
   | unique identifier (name ID 3) | 1.959 | 1.959;Scihe Math |

   A description (name ID 10) was added to the font, telling what it was
   derived from and how. The copyright notice, the version, the glyphs, their
   metrics and every other table (`MATH` included) are unchanged. The CFF
   table was written again by fontTools 4.63.0, which leaves out the default
   values `BlueShift 7` and `ForceBold false` from the private dictionary;
   their meaning is the same.
2. **Files.** Only the font is distributed, under a new name, with the
   license (as it is), this README and the manifest (written for Scihe Math,
   with new names as clause 1 requests). The documentation, the tests and
   their PDFs of the original package are not included; they are in the
   original on CTAN.

## How it was made

From `opentype/latinmodern-math.otf` of the CTAN package (lm-math.zip),
with [fontTools](https://github.com/fonttools/fonttools) 4.63.0:

```python
from fontTools.ttLib import TTFont

f = TTFont("latinmodern-math.otf")
name = f["name"]
new = {1: "Scihe Math", 3: "1.959;Scihe Math", 4: "SciheMath-Regular",
       6: "SciheMath-Regular", 16: "Scihe Math"}
for r in name.names:
    if r.nameID in new:
        r.string = new[r.nameID]
note = ("Derived from Latin Modern Math 1.959 by B. Jackowski, P. Strzelczyk and P. Pianowski "
        "(GUST e-foundry): only the font names were changed (to Scihe Math), as clause 1 of the "
        "GUST Font License requests; glyphs and tables are unchanged. Built into the scihe editor. "
        "The original: https://ctan.org/pkg/lm-math")
for plat, enc, lang in ((1, 0, 0), (3, 1, 0x409)):
    name.setName(note, 10, plat, enc, lang)
cff = f["CFF "].cff
cff.fontNames = ["SciheMath-Regular"]
top = cff.topDictIndex[0]
top.FullName = "SciheMath-Regular"
top.FamilyName = "Scihe Math"
f.save("scihe-math.otf")
```

Compared with the original, every table but `name`, `CFF ` and `head`
(its checksum and date) is the same byte for byte, and so are the 4802
glyph programs and the 1039 subroutines of `CFF `.

## Support

The authors of Latin Modern Math do not support Scihe Math or scihe. Report
problems with this font in scihe to scihe, not to them.
