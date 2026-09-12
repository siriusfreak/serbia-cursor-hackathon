from gen import *

# ---------- 1. PREDICTION — the inversion ----------
write("Prediction.dc.html", '<div class="wrap">' + card("#E8A33D", '''
  <div style="display:flex;justify-content:space-between;align-items:center">
    <div class="sec">PREDICT THE MAPPING</div>
    <div class="lvl">L1 &middot; transfer</div>
  </div>

  <div class="mapline">
    <span class="src">etcd</span>
    <span class="lnk">maps to</span>
    <span class="hole">?</span>
  </div>

  <div class="muted">shared role &middot; the single source of truth everything else reconciles against</div>

  <div class="hr"></div>

  <div class="body">What plays that role in an ML pipeline?</div>
  <div class="input">Type what you think it is&hellip;</div>
  <div class="btn">Commit answer</div>
  <div class="muted">You see the real mapping only after you commit to one. A wrong guess
    made first is worth more than a right one read off the page.</div>
''') + '</div>')

# ---------- 2. REVEAL ----------
write("Reveal.dc.html", '<div class="wrap">' + card("#4FBF8B", '''
  <div class="sec">YOU SAID</div>
  <div class="answered">model registry &mdash; it&rsquo;s where the versioned truth about a model lives</div>

  <div class="hr"></div>

  <div class="sec">THE MAPPING</div>
  <div class="mapline">
    <span class="src">etcd</span>
    <span class="lnk">maps to</span>
    <span class="tgt">feature store</span>
  </div>
  <div class="muted">shared role &middot; source of truth</div>

  <div class="body">Dual writes are split-brain. If the training job and the serving path
    read different definitions of <span style="color:#E8A33D">user_ltv_30d</span>, you have
    training-serving skew &mdash; the ML name for two controllers reconciling against
    different etcds.</div>

  <div class="row">''' + WARN + '''<div class="muted">This mapping has a limit,
    and it is the part that matters. You have not seen it yet.</div></div>

  <div class="btn">See where you diverged</div>
''') + '</div>')

# ---------- 3. CONTRAST — the step that teaches ----------
write("Contrast.dc.html", '<div class="wrap">' + card("#E8A33D", '''
  <div class="sec">WHERE YOU DIVERGED</div>

  <div style="display:flex;gap:14px;align-items:stretch">
    <div style="flex:1;background:#242932;border-radius:8px;padding:12px 14px;display:flex;flex-direction:column;gap:5px">
      <div class="muted">you said</div>
      <div class="body" style="font-weight:700">model registry</div>
    </div>
    <div style="flex:1;background:#242932;border-radius:8px;padding:12px 14px;display:flex;flex-direction:column;gap:5px">
      <div class="muted">holds that role</div>
      <div class="body" style="font-weight:700;color:#E8A33D">feature store</div>
    </div>
  </div>

  <div class="body">A model registry does hold versioned truth &mdash; but it fills the
    <span style="color:#E6E8EC;font-weight:600">rollback</span> role, not
    <span style="color:#E6E8EC;font-weight:600">source of truth</span>. Both are
    &ldquo;the versioned thing you trust&rdquo;, which is exactly why the two collapse
    together on the way across.</div>

  <div class="hr"></div>

  <div class="sec">WHERE THE ANALOGY BREAKS</div>
  <div class="row">''' + WARN + '''<div class="brk">Point-in-time correctness is an invariant
    etcd never had. A feature store must answer what was known <em>at request time</em>, or
    your training set leaks the future into itself.</div></div>

  <div style="display:flex;gap:12px">
    <div class="btn" style="flex:1">I see it</div>
    <div class="btn2" style="flex:1">Still unclear</div>
  </div>
  <div class="muted">&ldquo;Still unclear&rdquo; opens a misconception and brings this back tomorrow.</div>
''') + '</div>')
