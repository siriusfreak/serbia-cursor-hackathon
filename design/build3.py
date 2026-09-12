from gen import *

def prow(label, frac, right, rightcolor, fill):
    return ('<div style="display:flex;flex-direction:column;gap:6px">'
            '<div style="display:flex;justify-content:space-between;align-items:baseline">'
            '<span class="muted" style="color:#E6E8EC">%s</span>'
            '<span style="font-size:12px;font-weight:700;color:%s">%s</span></div>'
            '<div style="height:6px;border-radius:3px;background:#242932">'
            '<div style="height:6px;width:%s;border-radius:3px;background:%s"></div></div></div>'
            % (label, rightcolor, right, frac, fill))

feed = '''
  <div style="display:flex;justify-content:flex-end">
    <div style="background:#242932;border-radius:10px;padding:13px 16px;max-width:74%">
      <div class="body">I know Kubernetes, Go and distributed systems. I want to learn ML pipelines.</div>
    </div>
  </div>

  <div style="display:flex;gap:8px">''' + chip("profile_upsert") + chip("github_scan") + chip("analogy") + '''</div>

  <div class="body">Mapped three of your concepts onto ML infrastructure. Before I show you
    the table, answer one.</div>
''' + card("#E8A33D", '''
  <div style="display:flex;justify-content:space-between;align-items:center">
    <div class="sec">PREDICT THE MAPPING</div>
    <div class="lvl">L1</div>
  </div>
  <div class="mapline">
    <span class="src">etcd</span><span class="lnk">maps to</span><span class="hole">?</span>
  </div>
  <div class="muted">shared role &middot; the single source of truth everything reconciles against</div>
  <div class="body">What plays that role in an ML pipeline?</div>
  <div class="input">Type what you think it is&hellip;</div>
  <div class="btn">Commit answer</div>
''')

def group(label, rows, note=""):
    inner = "".join(rows)
    tail = ('<div class="muted">%s</div>' % note) if note else ""
    return ('<div style="display:flex;flex-direction:column;gap:10px">'
            '<div class="sec">%s</div>%s%s</div>' % (label, inner, tail))

# Two scales, drawn apart. A full amber bar and a near-full green bar are the
# same object meaning opposite things if they share a stack.
sidebar = ('<div style="display:flex;flex-direction:column;gap:20px">'
  + group("COGNITIVE DEBT",
      [prow("Go", "100%", "1.0", "#E8A33D", "#E8A33D"),
       prow("Python", "50%", "0.5", "#E8A33D", "#E8A33D")],
      "what you lean on and do not hold")
  + '<div class="hr"></div>'
  + group("MASTERY",
      [prow("Kubernetes", "82%", "82%", "#8C93A1", "#4FBF8B"),
       prow("distributed systems", "61%", "61%", "#8C93A1", "#4FBF8B")])
  + '<div class="hr"></div>'
  + group("OPEN MISCONCEPTIONS",
      ['<div class="muted" style="color:#E8A33D">3 &middot; one due today</div>'])
  + '<div class="hr"></div>'
  + group("PLUGINS",
      ['<div class="muted">%s</div>' % p for p in
        ["analogy 0.1.0 (agent)", "assessor 0.1.0 (assessor)",
         "github 0.1.0 (retrieval &middot; subprocess)", "profile 0.1.0 (profile)"]])
  + '</div>')

body = '''<div style="width:1180px;height:780px;display:flex;flex-direction:column;background:#14161A;overflow:hidden">

  <div style="display:flex;justify-content:space-between;align-items:center;padding:16px 22px">
    <div style="display:flex;flex-direction:column;gap:4px">
      <div style="font-size:16px;font-weight:700">cogdebt</div>
      <div class="muted">learn a new field through the one you already hold</div>
    </div>
    <div class="lvl" style="color:#E8A33D">grok-4.6</div>
  </div>
  <div class="hr"></div>

  <div style="flex:1;display:flex;min-height:0">
    <div style="flex:1;display:flex;flex-direction:column;gap:16px;padding:22px;overflow:hidden">''' + feed + '''</div>
    <div style="width:1px;background:#2C323C"></div>
    <div style="width:330px;padding:18px 16px;overflow:hidden">''' + sidebar + '''</div>
  </div>

  <div class="hr"></div>
  <div style="display:flex;gap:12px;padding:14px 22px">
    <div class="input" style="flex:1;min-height:0;padding:11px 14px">What do you already know?</div>
    <div class="btn" style="width:96px;padding:11px 0">Send</div>
  </div>
</div>'''

write("Main.dc.html", body)
