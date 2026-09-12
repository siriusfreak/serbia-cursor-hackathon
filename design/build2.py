from gen import *

# ---------- 4. FALSIFY ----------
write("Falsify.dc.html", '<div class="wrap">' + card("#E05A5A", '''
  <div style="display:flex;justify-content:space-between;align-items:center">
    <div class="sec">FALSIFY THE CLAIM</div>
    <div class="lvl">L2 &middot; the limit</div>
  </div>

  <div class="body">This sounds right. It is wrong. Say why.</div>

  <div style="background:#242932;border-radius:8px;padding:16px 18px;border:1px solid #2C323C">
    <div class="body" style="font-style:italic">&ldquo;Rolling back a bad model is
      <span style="font-family:ui-monospace,Menlo,monospace;font-style:normal;color:#E6E8EC">kubectl rollout undo</span>
      &mdash; point at the previous version and you are back where you were.&rdquo;</div>
  </div>

  <div class="input">Why does this fail?&hellip;</div>
  <div class="btn">Refute</div>
  <div class="muted">Written by the shallow analogy strategy on purpose. Spotting a
    plausible-but-false mapping is a harder test than recognising a true one.</div>
''') + '</div>')

# ---------- 5. MISCONCEPTION LEDGER ----------
def entry(target, source, claim, meta, color, retired=False):
    dim = "opacity:.55;" if retired else ""
    return ('<div style="%sbackground:#242932;border-radius:8px;padding:13px 15px;'
            'display:flex;flex-direction:column;gap:6px">'
            '<div class="mapline"><span class="src" style="%s">%s</span>'
            '<span class="lnk">borrowed from</span>'
            '<span class="lnk" style="color:#E6E8EC">%s</span></div>'
            '<div class="body" style="font-style:italic;color:%s">&ldquo;%s&rdquo;</div>'
            '<div class="muted">%s</div></div>'
            % (dim, "text-decoration:line-through" if retired else "", target, source, color, claim, meta))

write("Ledger.dc.html", '<div class="wrap">' + card("#E8A33D", '''
  <div class="sec">MISCONCEPTION LEDGER</div>
  <div class="muted">Debt this system issued. Every analogy lends an intuition; these are
    the ones still carried past the point where they hold.</div>

  <div class="hr"></div>
  <div class="sec" style="color:#E8A33D">OPEN &middot; 3</div>
''' + entry("model rollback", "Deployment rollout undo",
            "Rolling back the artifact restores the previous behaviour",
            "opened 2 days ago &middot; due today", "#E8A33D")
   + entry("feature store", "etcd",
            "The store is the generative truth, not a materialized view",
            "opened today &middot; due tomorrow", "#E8A33D")
   + entry("training job", "Kubernetes Job",
            "A failed run is retry-safe from scratch",
            "opened 5 days ago &middot; due in 2 days", "#E8A33D") + '''
  <div class="hr"></div>
  <div class="sec">RETIRED &middot; 2</div>
''' + entry("drift detection", "liveness probe",
            "A green probe means the model is still right",
            "retired 3 days ago &middot; answered L2 twice", "#8C93A1", True)
   + entry("pipeline step", "Pod",
            "Steps are stateless and cheap to restart",
            "retired last week &middot; answered L3", "#8C93A1", True)) + '</div>')
