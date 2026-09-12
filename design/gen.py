import pathlib
CSS = pathlib.Path("base.css").read_text()

SHELL = '''<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <script src="./support.js"></script>
</head>
<body>
<x-dc>
<helmet>
  <style>
%s
  </style>
</helmet>
%s
</x-dc>
</body>
</html>
'''

WARN = ('<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#E8A33D" '
        'stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex:none;margin-top:3px">'
        '<path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/>'
        '<path d="M12 9v4"/><path d="M12 17h.01"/></svg>')

def gear(c="#8C93A1"):
    return ('<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="%s" '
            'stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex:none">'
            '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6h.09A1.65 1.65 0 0 0 10.6 3.09V3a2 2 0 1 1 4 0v.09A1.65 1.65 0 0 0 15 4.6a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9v.09c.14.6.66 1.03 1.28 1.06H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>' % c)

def chip(name):
    return ('<span class="chip" style="display:inline-flex;align-items:center;gap:6px">'
            + gear() + name + '</span>')

def card(bar, inner, extra=""):
    return ('<div class="acc"%s><div class="bar" style="background:%s"></div>'
            '<div class="card">%s</div></div>' % (extra, bar, inner))

def write(name, body):
    pathlib.Path(name).write_text(SHELL % (CSS, body))
    print("wrote", name)
