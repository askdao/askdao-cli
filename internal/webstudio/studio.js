    const STEPS = [{ label: "Identity" }, { label: "Persona" }, { label: "Skills & Tools" }, { label: "Review & Deploy" }];
    // Models come from conductor, NOT a hardcoded table — concrete model ids and
    // prices live only in conductor (admin-maintained whitelist, cloud#84).
    //   data.models        — `models[]` whitelist across both harnesses: the step-2
    //                        dropdown; picking a model also picks the deploy harness.
    //   data.model_catalog — legacy three-tier `classes[]`: rendered only when the
    //                        whitelist is empty (offline / old conductor); the
    //                        offline fallback has no model_id (binary stays id-free).
    const modelEntry = (slug) => (data.model_catalog || []).find(c => c.slug === slug);
    const models = () => data.models || [];
    const modelById = (id) => models().find(m => m.model_id === id);
    const HARNESS_ANTHROPIC = 'anthropic_managed_agents', HARNESS_OPENAI = 'openai_agents_sdk';
    const HARNESS_LABEL = { [HARNESS_ANTHROPIC]: 'Anthropic Managed Agents', [HARNESS_OPENAI]: 'OpenAI Agents SDK' };
    // Step 2 dropdown groups (#90): show model families, never the vendor we route through.
    // anthropic → Anthropic, openai → OpenAI, anything else (SiliconFlow-served DeepSeek/Kimi/
    // MiniMax/GLM, direct deepseek/moonshot …) → Open Source. Fixed order.
    const MODEL_GROUPS = ['Anthropic', 'OpenAI', 'Open Source'];
    const groupOf = (m) => m.provider === 'anthropic' ? 'Anthropic' : m.provider === 'openai' ? 'OpenAI' : 'Open Source';
    const currentHarness = () => spec.preferred_harness || HARNESS_ANTHROPIC;
    const isOpenAI = () => currentHarness() === HARNESS_OPENAI;
    const fmtUsd = (v) => v == null ? '—' : '$' + (+v).toFixed(+v >= 10 ? 2 : 3).replace(/(\.\d*?[1-9])0+$|\.0+$/, '$1');
    // Which whitelist entry the draft currently means: explicit model_preferences
    // pin → the tier's model on the draft's harness → that harness's recommended.
    function currentModelId() {
      const ms = models(); if (!ms.length) return '';
      const pref = (spec.persona.model_preferences || [])[0];
      if (pref && modelById(pref.id)) return pref.id;
      const h = currentHarness(), cls = (spec.persona.model_class || '').toLowerCase();
      const byClass = ms.find(m => m.harness_id === h && m.model_class === cls);
      if (byClass) return byClass.model_id;
      const rec = ms.find(m => m.harness_id === h && m.recommended) || ms.find(m => m.harness_id === h);
      return (rec || ms[0]).model_id;
    }
    function modelOptionsHTML(selected) {
      const byGroup = new Map(MODEL_GROUPS.map(g => [g, []]));
      models().forEach(m => byGroup.get(groupOf(m)).push(m));
      const groups = MODEL_GROUPS.filter(g => byGroup.get(g).length).map(g => ({ label: g, items: byGroup.get(g) }));
      return groups.map(g => `<optgroup label="${esc(g.label)}">` + g.items.map(m =>
        `<option value="${esc(m.model_id)}"${m.model_id === selected ? ' selected' : ''}>${esc(m.display_name)} · ${fmtUsd(m.input_usd_per_mtok)} / ${fmtUsd(m.output_usd_per_mtok)} per MTok${m.recommended ? ' · recommended' : ''}</option>`
      ).join('') + '</optgroup>').join('');
    }
    function modelHintHTML(key) {
      if (models().length) {
        const m = modelById(key);
        if (!m) return '';
        return `<b>${esc(m.display_name)}</b> <span class="mono">(${esc(m.model_id)})</span> · runs on ${esc(HARNESS_LABEL[m.harness_id] || m.harness_id)}`
          + (m.blurb ? `<br>${esc(m.blurb)}` : '')
          + `<br><span class="price">input ${fmtUsd(m.input_usd_per_mtok)} · output ${fmtUsd(m.output_usd_per_mtok)} per 1M tokens${m.cached_input_usd_per_mtok != null ? ` · cached input ${fmtUsd(m.cached_input_usd_per_mtok)}` : ''}</span>`;
      }
      const c = modelEntry(key);
      if (!c) return '';
      if (!c.model_id) return esc(c.blurb || '');
      const cost = c.cost_tier && c.cost_tier !== 'unknown' ? ` · ${esc(c.cost_tier)} cost` : '';
      return `${esc(c.friendly_name)} <span class="mono">(${esc(c.model_id)})</span>${cost}`;
    }
    function reviewModelHTML() {
      if (models().length) {
        const m = modelById(currentModelId());
        if (!m) return '<span class="empty">—</span>';
        return `${esc(m.display_name)} <span class="mono">(${esc(m.model_id)})</span> · ${fmtUsd(m.input_usd_per_mtok)} / ${fmtUsd(m.output_usd_per_mtok)} per MTok`;
      }
      const slug = spec.persona.model_class, c = modelEntry(slug);
      if (!c) return esc(slug || 'balanced');
      if (!c.model_id) return esc(c.label);
      return `${esc(c.label)} · ${esc(c.friendly_name)} <span class="mono">(${esc(c.model_id)})</span>`;
    }
    // Write the picked whitelist model into the spec: explicit pin (provider from
    // the catalog, never hardcoded), the tier it represents (conductor still
    // requires model_class), and the harness it runs on. Returns true when the
    // harness changed (Step 3 gates need re-syncing).
    function applyModel(id) {
      const m = modelById(id); if (!m) return false;
      spec.persona.model_preferences = [{ provider: m.provider, id: m.model_id }];
      if (m.model_class) spec.persona.model_class = m.model_class;
      const changed = currentHarness() !== m.harness_id;
      spec.preferred_harness = m.harness_id;
      return changed;
    }
    function pickModel(id) {
      const changed = applyModel(id);
      const hint = $('modelHint');
      if (hint) hint.innerHTML = modelHintHTML(id);
      if (changed) syncHarnessGates();
    }
    function pickModelClass(slug) {
      spec.persona.model_class = slug;
      const hint = $('modelHint');
      if (hint) hint.innerHTML = modelHintHTML(slug);
    }
    // Harness-dependent gating (cloud#84): the backup runtime runs the KOL's OWN skills
    // since conductor #372 (unpacked into the sandbox + indexed in the system prompt), but
    // Anthropic's built-in skills (xlsx / pdf …) are a platform feature it can't provide —
    // so Step 3 only speaks up when a built-in is actually ticked. (Scheduled runs work on
    // both runtimes since conductor #352 — no schedule gate here.)
    function tickedBuiltinSkills() {
      return (data.skill_candidates || []).filter((c, i) => {
        const el = $('skill-' + i);
        return c.builtin && el && el.checked;
      });
    }
    function syncHarnessGates() {
      const sk = $('harnessSkillsNote');
      if (!sk) return;
      const builtins = isOpenAI() ? tickedBuiltinSkills() : [];
      sk.hidden = !builtins.length;
      sk.textContent = 'Built-in Anthropic skills (' + builtins.map(c => c.name).join(', ')
        + ') do not run on this runtime — they stay in the yaml but are skipped at deploy. '
        + 'Your own skills DO run here. Pick a Claude model in Step 2 to use the built-ins.';
    }
    const HARNESS_NAME = { claude: "Claude Code", codex: "OpenAI Codex", cursor: "Cursor", cowork: "Claude Cowork" };
    let data, spec, palette = {}, step = 0, vault = [];
    const $ = (id) => document.getElementById(id);
    const esc = (s) => (s == null ? "" : String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])));
    const hLabel = (h) => HARNESS_NAME[h] || h;

    // confidence badge — finds the reasoning whose decision matches a keyword and
    // renders an inline AI-score chip (green/amber/red) with the reason on hover.
    function confBadge(...keys) {
      const r = (data.reasoning || []).find(x => keys.some(k => (x.decision || '').toLowerCase().includes(k)));
      if (!r || r.confidence == null) return '';
      const c = r.confidence, cls = c >= 0.8 ? 'hi' : c >= 0.5 ? 'mid' : 'lo', pct = Math.round(c * 100);
      return `<span class="conf ${cls}" data-tip="${esc(r.reason)}">AI ${pct}%</span>`;
    }

    async function load() {
      data = await (await fetch('/api/spec')).json();
      spec = data.spec || {}; spec.metadata = spec.metadata || {}; spec.persona = spec.persona || {};
      (data.palette || []).forEach(p => palette[p.token] = p.hex);
      initVault();
      $('projName').textContent = data.project_name || 'agent';
      applyAccent(spec.metadata.theme_color);
      renderNav(); renderPanels(); go(0);
      syncHarnessGates();
      startObservePolling();
      if (data.desktop) { initAuth(); $('assistantFab').hidden = false; if (data.needs_scan) showScanPrompt(); else validateSkills(); }
    }

    // Desktop-only: the pre-scan placeholder shows a folder-picker overlay. POST
    // /api/scan opens a native picker (Go side), scans on-device, then we reload
    // the workbench with the scanned draft.
    function showScanPrompt() { $('scanOverlay').hidden = false; }
    // Split scan: pick opens the native dialog and returns the path immediately, so we
    // show it + a Stop button before the (cancellable) run. Stop → /api/scan/cancel
    // aborts the pipeline mid-flight and the run fetch rejects back to the picker.
    async function doScan() {
      let dir;
      try {
        const p = await (await fetch('/api/scan/pick', { method: 'POST' })).json();
        if (p && p.error) throw new Error(p.error);
        dir = p && p.dir;
      } catch (e) { $('scanStatus').textContent = 'Could not open the folder picker: ' + (e.message || e); return; }
      if (!dir) return; // user cancelled the dialog — leave the picker as-is
      $('chooseFolderBtn').hidden = true;
      $('stopScanBtn').hidden = false;
      $('scanStatus').textContent = 'Scanning ' + dir + ' …';
      try {
        const r = await (await fetch('/api/scan/run', { method: 'POST' })).json();
        if (r && r.error) throw new Error(r.error);
        $('scanOverlay').hidden = true;
        $('scanStatus').textContent = '';
        resetScanPrompt();
        await load();
      } catch (e) {
        resetScanPrompt();
        $('scanStatus').textContent = 'Scan stopped or failed: ' + (e.message || e);
      }
    }
    async function stopScan() {
      $('stopScanBtn').disabled = true;
      $('scanStatus').textContent = 'Stopping…';
      try { await fetch('/api/scan/cancel', { method: 'POST' }); } catch (e) { /* the run fetch's catch resets the UI */ }
    }
    function resetScanPrompt() { $('chooseFolderBtn').hidden = false; $('stopScanBtn').hidden = true; $('stopScanBtn').disabled = false; }

    // Desktop-only login panel (data.desktop). Uses webstudio's /api/auth/* which
    // exist ONLY when the desktop host injects auth callbacks — the CLI never has
    // these routes, so agent edit is unaffected. Device flow: Sign in → POST /login
    // shows a user code + opens the browser → poll /poll until approved.
    let authPollTimer = null;
    async function initAuth() { $('loginbar').hidden = false; await refreshAuth(); }
    async function refreshAuth() {
      try { renderLogin(await (await fetch('/api/auth/status')).json()); }
      catch (e) { $('loginbar').innerHTML = '<span style="color:#b45">auth unavailable</span>'; }
    }
    function renderLogin(s) {
      if (s && s.logged_in) {
        $('loginbar').innerHTML = `<span>✓ ${esc(s.email || 'signed in')}</span> <button class="act btn" onclick="doLogout()">Sign out</button>`;
      } else {
        $('loginbar').innerHTML = `<button class="act btn primary" onclick="doLogin()">Sign in to AskDAO</button>`;
      }
    }
    async function doLogin() {
      $('loginbar').innerHTML = '<span>starting…</span>';
      try {
        const ch = await (await fetch('/api/auth/login', { method: 'POST' })).json();
        if (ch.error) throw new Error(ch.error);
        $('loginbar').innerHTML = `<span>code <b>${esc(ch.user_code)}</b> · browser opened, approve to finish…</span>`;
        if (authPollTimer) clearInterval(authPollTimer);
        authPollTimer = setInterval(pollAuth, 3000);
      } catch (e) {
        $('loginbar').innerHTML = `<span style="color:#b45">login failed</span> <button class="act btn" onclick="doLogin()">Retry</button>`;
      }
    }
    async function pollAuth() {
      try {
        const s = await (await fetch('/api/auth/poll')).json();
        if (s.error) { clearInterval(authPollTimer); $('loginbar').innerHTML = `<span style="color:#b45">${esc(s.error)}</span> <button class="act btn" onclick="doLogin()">Retry</button>`; return; }
        if (s.logged_in) { clearInterval(authPollTimer); renderLogin(s); }
      } catch (e) { /* transient — keep polling */ }
    }
    async function doLogout() {
      try { await fetch('/api/auth/logout', { method: 'POST' }); } catch (e) { }
      refreshAuth();
    }

    function initVault() {
      const v = spec.vault_hints || {};
      // Scanned credentials become read-only tick rows (like skill/mcp candidates),
      // default checked — vault_hints is already the scanner-filtered credential set.
      // required/optional stays per-row for collect() to split the buckets; no control.
      vault = [
        ...(v.required_credentials || []).map(c => ({ name: c.name || '', purpose: c.purpose || '', required: true, checked: true, readonly: true })),
        ...(v.optional_credentials || []).map(c => ({ name: c.name || '', purpose: c.purpose || '', required: false, checked: true, readonly: true })),
      ];
    }
    function renderNav() {
      $('stepNav').innerHTML = STEPS.map((s, i) =>
        `<li data-i="${i}" onclick="go(${i})"><span class="dot">${i + 1}</span><span class="st">Step ${i + 1}</span><span class="lb">${s.label}</span></li>`).join('');
    }
    function renderPanels() { $('panels').innerHTML = panelIdentity() + panelPersona() + panelSkills() + panelReview(); renderIconGrid(); renderAvatar(); refreshSched(); }

    /* Step 1 · Identity */
    function panelIdentity() {
      const m = spec.metadata;
      const cats = (data.categories || []).map(c => `<option ${c === m.category ? 'selected' : ''}>${esc(c)}</option>`).join('');
      // Output language: ISO 639-1 code; '' = universal/adaptive (agent follows the
      // user's language, shows under every discover language filter). UI lists the
      // two launch languages; the yaml field accepts any two-letter code by hand.
      const langs = [['', 'Auto · follows the user'], ['zh', '中文 Chinese'], ['en', 'English']].map(([v, l]) =>
        `<option value="${v}" ${v === (m.language || '') ? 'selected' : ''}>${l}</option>`).join('');
      const vis = [['private', 'only you'], ['shared', 'your subscribers'], ['public', 'anyone']].map(([v, h]) =>
        `<label><input type="radio" name="vis" value="${v}" ${v === (m.visibility || 'private') ? 'checked' : ''} onchange="spec.metadata.visibility=this.value;syncSchedAudience()"><span>${v}<small style="display:block;font-size:11px;color:var(--stone);font-weight:400;margin-top:2px">${h}</small></span></label>`).join('');
      const sw = (data.palette || []).map(t =>
        `<div class="sw ${t.token === m.theme_color ? 'sel' : ''}" style="background:${t.hex};color:${t.hex}" title="${t.label}" onclick="pickTheme('${t.token}')"></div>`).join('');
      return `<section class="panel" data-step="0">
    <p class="eyebrow">Step 1 of 4 · Identity</p>
    <h2>Give your agent an identity</h2>
    <p class="sub">Name it, describe what it's for, and give it a face.</p>
    <div class="card">
      <label class="f"><span>Agent name</span><input type="text" id="f_name" value="${esc(m.name)}" placeholder="my-agent"><div class="field-err" id="err_name"></div></label>
      <label class="f"><span>Display name · what subscribers see ${confBadge('display_name', 'display')}</span><input type="text" id="f_display" value="${esc(m.display_name || '')}" placeholder="${esc(m.name) || 'e.g. 投资军师'}"><div class="field-err" id="err_display"></div></label>
      <label class="f"><span>Description · what it does, who it's for ${confBadge('description', 'desc')}</span>
        <textarea id="f_desc" class="desc" placeholder="e.g. A spelling tutor for grade-school homework — it spots errors, explains the rules, and gives practice exercises.">${esc(m.description)}</textarea><div class="field-err" id="err_desc"></div></label>
      <label class="f"><span>Category ${confBadge('category')}</span><select id="f_cat">${cats}</select></label>
      <label class="f"><span>Output language · which audience it speaks to</span><select id="f_lang">${langs}</select></label>
    </div>
    <div class="card">
      <p class="card-head">Theme color ${confBadge('theme_color', 'theme', 'color')}</p>
      <p class="card-note">The brand color subscribers see on this agent's chat &amp; group page.</p>
      <div class="swatches">${sw}</div>
    </div>
    <div class="card">
      <p class="card-head">Avatar ${confBadge('avatar', 'icon')}</p>
      <p class="card-note">A face for your agent — pick an icon (tinted with your theme color) or keep the initial.</p>
      <div class="av-row">
        <div id="avPreview" class="av-preview"></div>
        <div id="avGrid" class="av-grid"></div>
      </div>
    </div>
    <div class="card">
      <p class="card-head">Visibility ${confBadge('visibility', 'visible', 'access')}</p>
      <p class="card-note">Who can find and use this agent once it's deployed.</p>
      <div class="seg">${vis}</div>
    </div>
  </section>`;
    }

    /* Step 2 · Persona */
    function panelPersona() {
      const p = spec.persona;
      const catalog = data.model_catalog || [];
      let picker, hintKey;
      if (models().length) {
        // Whitelist dropdown (cloud#84): most-common first, grouped by provider,
        // raw $/MTok on every option; the selection also fixes preferred_harness.
        const cur = currentModelId();
        applyModel(cur);
        hintKey = cur;
        picker = `<select id="modelSel" class="model-select" onchange="pickModel(this.value)">${modelOptionsHTML(cur)}</select>`;
      } else {
        // Offline / old conductor: legacy three-tier pills. Default to the
        // recommended tier when the draft's model_class isn't offered (legacy slugs).
        if (catalog.length && !catalog.some(c => c.slug === p.model_class)) {
          p.model_class = (catalog.find(c => c.recommended) || catalog[0]).slug;
        }
        hintKey = p.model_class;
        picker = `<div class="seg">${catalog
          .map(c => `<label><input type="radio" name="mc" value="${esc(c.slug)}" ${c.slug === p.model_class ? 'checked' : ''} onchange="pickModelClass(this.value)"><span>${esc(c.label)}</span></label>`).join('')}</div>`;
      }
      return `<section class="panel" data-step="1">
    <p class="eyebrow">Step 2 of 4 · Persona</p>
    <h2>Shape its voice</h2>
    <p class="sub">Pick the model and write the system prompt that defines its behaviour.</p>
    <div class="card"><label class="f"><span>Model ${confBadge('model')}</span>${picker}<div class="model-hint" id="modelHint">${modelHintHTML(hintKey)}</div><div class="field-err" id="err_model"></div></label></div>
    <div class="card"><label class="f"><span>System prompt ${confBadge('persona', 'system', 'prompt')}</span>
      <textarea id="f_prompt" class="prompt" placeholder="You are a …">${esc(p.system_prompt)}</textarea><div class="field-err" id="err_prompt"></div></label></div>
    ${memoryKnowledgeCardHTML()}
  </section>`;
    }

    /* Memory & Knowledge — four conductor-side switches. Declared here = applied
       on every deploy (this file is the source of truth); an undeclared block is
       left untouched so the web dashboard keeps owning it. */
    function memoryKnowledgeCardHTML() {
      const m = spec.memory || {}, w = spec.wiki || {};
      const on = v => v === true ? ' checked' : '';
      const row = (id, checked, extra, nm, ds) =>
        `<label class="item"><input type="checkbox" id="${id}"${checked}${extra} onchange="toggleKnowledge()"><div><div class="nm">${nm}</div><div class="ds">${ds}</div></div></label>`;
      const wikiOn = w.enabled === true;
      return `<div class="card"><div class="f"><span>Memory &amp; knowledge</span>
      <div class="ds" style="margin:-2px 0 10px">Applied on every deploy — this file is the source of truth. The web dashboard can flip them between deploys.</div>
      ${row('f_mem_user', on(m.inject_user_profile), '', 'Inject user profile',
        "Give the agent the user's consolidated cross-agent profile, so it knows their long-term background.")}
      ${row('f_mem_agent', on(m.inject_agent_profile), '', 'Inject agent-specific profile',
        'Give the agent the profile built from this user&rsquo;s prior conversations with it.')}
      ${row('f_wiki_on', on(w.enabled), '', 'Enable agent wiki',
        'The agent keeps a per-user knowledge wiki it can search, read and write during conversations.')}
      ${row('f_wiki_evo', on(w.evolution), wikiOn ? '' : ' disabled', 'Enable wiki evolution',
        'A periodic run compiles recent conversations into the wiki and prunes stale pages. Requires the wiki.')}
    </div></div>`;
    }

    function toggleKnowledge() {
      // Evolution is meaningless without the wiki; keep the UI honest even
      // though the server stores the two columns independently.
      const w = $('f_wiki_on'), e = $('f_wiki_evo');
      if (!w || !e) return;
      e.disabled = !w.checked;
      if (!w.checked) e.checked = false;
    }

    /* Step 3 · Skills & Tools (tabs: Skills / MCP / Secrets) */
    function panelSkills() {
      const sc = (data.skill_candidates || []).length, mc = (data.mcp_candidates || []).length;
      return `<section class="panel" data-step="2">
    <p class="eyebrow">Step 3 of 4 · Skills &amp; Tools</p>
    <h2>Choose its capabilities</h2>
    <p class="sub">Only what you tick travels with the agent.</p>
    ${observeBar()}
    <div class="tabs">
      <button type="button" class="tab active" onclick="switchTab('skills',this)">Skills <span class="cnt">${sc}</span></button>
      <button type="button" class="tab" onclick="switchTab('mcp',this)">MCP servers <span class="cnt">${mc}</span></button>
      <button type="button" class="tab" onclick="switchTab('vault',this)">Secrets <span class="cnt" id="vaultCnt">${vault.length}</span></button>
      <button type="button" class="tab" onclick="switchTab('automation',this)">Automation <span class="cnt">${(spec.outcomes ? 1 : 0) + (spec.schedule ? 1 : 0)}</span></button>
    </div>
    <div class="tabpane" data-pane="skills">${skillFlagBar()}<div class="harness-note" id="harnessSkillsNote" hidden></div><div class="card">${skillGroups()}</div></div>
    <div class="tabpane" data-pane="mcp" hidden><div class="card">${mcpGroups()}</div></div>
    <div class="tabpane" data-pane="vault" hidden><div class="card">${vaultPaneHTML()}</div></div>
    <div class="tabpane" data-pane="automation" hidden><div class="card">${automationPaneHTML()}</div></div>
  </section>`;
    }
    /* Automation tab (#303): outcomes (delivery-acceptance rubric) + schedule
       (recurring run). Both optional; values restore from spec on edit. */
    const NOTIFY_CHANNELS = ['', 'telegram', 'wechat', 'imessage', 'feishu', 'dingtalk', 'whatsapp'];
    const TZ_PRESETS = ['UTC', 'Asia/Shanghai', 'Asia/Tokyo', 'Asia/Singapore', 'Europe/London', 'Europe/Paris', 'America/New_York', 'America/Los_Angeles'];
    const SCHED_MODES = [['daily', 'Daily'], ['weekly', 'Weekly'], ['monthly', 'Monthly'], ['interval', 'Interval'], ['custom', 'Custom cron']];
    const DOW_LABEL = ['S', 'M', 'T', 'W', 'T', 'F', 'S'];
    const DOW_NAME = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
    const INTERVAL_STEPS = { minutes: [1, 2, 5, 10, 15, 20, 30], hours: [1, 2, 3, 4, 6, 8, 12] };
    const INTERVAL_DEFAULT = { minutes: 15, hours: 1 };
    /* Alarm-style schedule picker state (#303). #f_sc_cron holds the one
       serialized value — collect() reads it, conductor/PG store it — and `sched`
       only drives the controls. The cron is written back on user interaction and
       never on render: re-opening a spec must leave an untouched schedule
       byte-exact, both so an unvisited pane doesn't grow a schedule: key and
       because render.DiffAgentSpec compares the raw string against
       .askdao/recommendation.yml (a normalized "0 9 * * 1" would read as an edit). */
    // Schedule audience (conductor #326): personal = every user runs their own
    // copy (the Builder's cadence is their own row + the default seeded into a
    // subscriber's row); broadcast = the Builder runs once into the agent's
    // Group and members get an IM notice. broadcast needs an audience, so it
    // is greyed out while visibility is private (conductor rejects it anyway).
    const SCHED_AUDIENCES = [
      ['personal', 'Personal', 'Each subscriber runs their own copy on their own cadence and credit'],
      ['broadcast', 'Broadcast', 'You run once; the result goes to your subscriber Group and their IM'],
    ];
    function schedAudience() {
      const r = document.querySelector('input[name="scaud"]:checked');
      return r ? r.value : 'personal';
    }
    function pickSchedAudience(v) { syncSchedAudience(); }
    function syncSchedAudience() {
      const seg = $('scAud');
      if (!seg) return;
      const priv = (spec.metadata.visibility || 'private') === 'private';
      const bc = seg.querySelector('label[data-aud="broadcast"]');
      bc.classList.toggle('off', priv);
      bc.querySelector('input').disabled = priv;
      if (priv && schedAudience() === 'broadcast') seg.querySelector('input[value="personal"]').checked = true;
      const aud = schedAudience();
      $('scAudNote').textContent = priv
        ? 'Broadcast needs subscribers — set visibility to shared or public first.'
        : aud === 'broadcast'
          ? 'One run, billed to you; members read it in the Group and get an IM notice (they can mute the Group).'
          : 'Subscribers who opt in pay for their own runs and pick their own time; your task text is shared, your cadence is only their default.';
      $('scRepeatLbl').textContent = aud === 'personal' && !priv ? 'Your cadence (subscribers set their own)' : 'Repeat';
    }
    let sched = { mode: 'daily', hour: 9, min: 0, dows: [1], doms: [1], every: 15, unit: 'minutes' };
    function automationPaneHTML() {
      const o = spec.outcomes || {}, s = spec.schedule || {};
      const oOn = !!spec.outcomes && o.enabled !== false;
      const sOn = !!spec.schedule && s.enabled !== false;
      const tz = s.timezone || 'UTC';
      const nOpts = NOTIFY_CHANNELS.map(c => `<option value="${c}"${c === (s.notify_channel || '') ? ' selected' : ''}>${c || 'Most recent IM binding (default)'}</option>`).join('');
      // Restore the picker from the stored cron; anything the picker can't
      // express (0 9 * * 1,4 · */15 9-17 * * 1-5 · @daily) lands in Custom with
      // the string untouched rather than being rounded to the nearest preset.
      const parsed = s.cron ? schedFromCron(s.cron) : null;
      if (parsed) Object.assign(sched, parsed);
      sched.mode = s.cron ? (parsed ? parsed.mode : 'custom') : 'daily';
      const row = (modes, body) => `<div class="sched-row" data-scm="${modes}"${modes.split(' ').indexOf(sched.mode) < 0 ? ' hidden' : ''}>${body}</div>`;
      return `<div class="grp"><h3>Delivery acceptance (Outcomes)</h3>
    <label class="item"><input type="checkbox" id="f_oc_on" ${oOn ? 'checked' : ''} onchange="toggleAutomation()"><div><div class="nm">Grade deliverables against a rubric</div><div class="ds">After the agent delivers files, a grader checks them against your rubric and drives revisions (billed like extra turns).</div></div></label>
    <div id="ocFields" ${oOn ? '' : 'hidden'}>
      <label class="lbl" for="f_oc_rubric">Acceptance rubric (markdown, one criterion per line)</label>
      <textarea id="f_oc_rubric" rows="6" placeholder="- Covers all merged PRs this week&#10;- Output is a single xlsx file">${esc(o.rubric || '')}</textarea>
      <div class="field-err" id="err_oc_rubric"></div>
      <label class="lbl" for="f_oc_iters">Max grader iterations (1-5)</label>
      <input id="f_oc_iters" type="number" min="1" max="5" value="${o.max_iterations || 3}" style="width:90px">
    </div></div>
    <div class="grp"><h3>Recurring run (Schedule)</h3>
    <label class="item"><input type="checkbox" id="f_sc_on" ${sOn ? 'checked' : ''} onchange="toggleAutomation()"><div><div class="nm">Run this agent on a schedule</div><div class="ds">Each run starts a fresh session, bills your credit, and pushes the result to your IM.</div></div></label>
    <div id="scFields" ${sOn ? '' : 'hidden'}>
      <div class="lbl">Run for</div>
      <div class="seg" id="scAud">${SCHED_AUDIENCES.map(a => `<label data-aud="${a[0]}" title="${a[2]}"><input type="radio" name="scaud" value="${a[0]}"${(s.mode || 'personal') === a[0] ? ' checked' : ''} onchange="pickSchedAudience('${a[0]}')">${a[1]}</label>`).join('')}</div>
      <div class="sched-note" id="scAudNote"></div>
      <div class="lbl" id="scRepeatLbl">Repeat</div>
      <div class="seg">${SCHED_MODES.map(m => `<label><input type="radio" name="scmode" value="${m[0]}"${sched.mode === m[0] ? ' checked' : ''} onchange="pickSchedMode('${m[0]}')">${m[1]}</label>`).join('')}</div>
      ${row('weekly', `<div class="lbl">On these days</div>
      <div class="chips" id="scDows">${DOW_LABEL.map((l, i) => `<button type="button" class="chip${sched.dows.indexOf(i) < 0 ? '' : ' on'}" data-v="${i}" title="${DOW_NAME[i]}" onclick="toggleDow(${i})">${l}</button>`).join('')}</div>
      <div class="chip-quick"><button type="button" onclick="quickDows('wd')">Weekdays</button><button type="button" onclick="quickDows('all')">Every day</button></div>`)}
      ${row('monthly', `<div class="lbl">On these dates</div>
      <div class="chips dates" id="scDoms">${Array.from({ length: 31 }, (_, k) => k + 1).map(d => `<button type="button" class="chip${sched.doms.indexOf(d) < 0 ? '' : ' on'}" data-v="${d}" onclick="toggleDom(${d})">${d}</button>`).join('')}</div>
      <div class="sched-note" id="scDomNote"${sched.doms[sched.doms.length - 1] > 28 ? '' : ' hidden'}>Months without that date are skipped — the 31st only fires in 31-day months.</div>`)}
      ${row('interval', `<div class="lbl">How often</div>
      <div class="sched-inline">Every <select id="f_sc_every" onchange="pickInterval(false)">${intervalOptsHTML(sched.unit, sched.every)}</select>
      <select id="f_sc_unit" onchange="pickInterval(true)"><option value="minutes"${sched.unit === 'minutes' ? ' selected' : ''}>minutes</option><option value="hours"${sched.unit === 'hours' ? ' selected' : ''}>hours</option></select></div>`)}
      ${row('daily weekly monthly', `<div class="lbl">At</div>${wheelsHTML()}`)}
      ${row('custom', `<label class="lbl" for="f_sc_cron">Cron expression (5 fields: minute hour day month weekday)</label>
      <input id="f_sc_cron" type="text" placeholder="0 9 * * 1" value="${esc(s.cron || '')}" oninput="renderSchedSummary()" style="font-family:var(--mono)">
      <div class="field-err" id="err_sc_cron"></div>`)}
      <label class="lbl" for="f_sc_tz">Time zone</label>
      <select id="f_sc_tz" onchange="renderSchedSummary()">${tzOptsHTML(tz)}</select>
      <div class="sched-sum" id="scSum" hidden></div>
      <label class="lbl" for="f_sc_task">Task instruction (what each run should do)</label>
      <textarea id="f_sc_task" rows="4" placeholder="Generate last week's report and deliver it as xlsx">${esc(s.task || '')}</textarea>
      <div class="field-err" id="err_sc_task"></div>
      <label class="lbl" for="f_sc_notify">Send results to</label>
      <select id="f_sc_notify">${nOpts}</select>
    </div></div>`;
    }
    function toggleAutomation() {
      const oc = $('f_oc_on'), sc = $('f_sc_on');
      if ($('ocFields')) $('ocFields').hidden = !(oc && oc.checked);
      if ($('scFields')) $('scFields').hidden = !(sc && sc.checked);
      // Seed on enable, never on render: a brand-new schedule gets daily 9:00 in
      // this machine's own zone the moment it is switched on; an existing spec
      // keeps whatever it already stored.
      if (sc && sc.checked) {
        const el = $('f_sc_cron'), tzs = $('f_sc_tz'), loc = localTZ();
        if (el && !el.value.trim()) {
          if (tzs && tzs.value === 'UTC' && loc !== 'UTC' && [].some.call(tzs.options, o => o.value === loc)) tzs.value = loc;
          syncSched();
        }
      }
      refreshSched();
    }
    const localTZ = () => { try { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'; } catch (e) { return 'UTC'; } };
    function tzOptsHTML(cur) {
      let all = [];
      try { all = Intl.supportedValuesOf('timeZone') || []; } catch (e) { all = []; }
      if (!all.length) all = TZ_PRESETS;
      const top = [], seen = {};
      [cur, localTZ(), 'UTC'].forEach(z => { if (z && !seen[z]) { seen[z] = 1; top.push(z); } });
      const opt = z => `<option value="${esc(z)}"${z === cur ? ' selected' : ''}>${esc(z)}</option>`;
      return `<optgroup label="Suggested">${top.map(opt).join('')}</optgroup><optgroup label="All time zones">${all.filter(z => !seen[z]).map(opt).join('')}</optgroup>`;
    }
    const intervalOptsHTML = (unit, cur) => INTERVAL_STEPS[unit].map(n => `<option value="${n}"${n === cur ? ' selected' : ''}>${n}</option>`).join('');

    /* ---- picker controls ---------------------------------------------- */
    function pickSchedMode(m) {
      sched.mode = m;
      document.querySelectorAll('input[name=scmode]').forEach(r => { r.checked = r.value === m; });
      document.querySelectorAll('.sched-row').forEach(r => { r.hidden = r.dataset.scm.split(' ').indexOf(m) < 0; });
      // Leaving Custom hands authorship back to the picker; entering it keeps
      // the current string so the KOL edits what they were just looking at.
      if (m === 'custom') { const el = $('f_sc_cron'); if (el && !el.value.trim()) el.value = cronFromSched(); }
      else syncSched();
      refreshSched();
    }
    function paintChips(id, sel) {
      const box = $(id);
      if (box) box.querySelectorAll('.chip').forEach(c => c.classList.toggle('on', sel.indexOf(+c.dataset.v) >= 0));
    }
    function toggleDow(i) {
      const k = sched.dows.indexOf(i);
      if (k >= 0) { if (sched.dows.length === 1) return; sched.dows.splice(k, 1); } else sched.dows.push(i);
      sched.dows.sort((a, b) => a - b);
      paintChips('scDows', sched.dows);
      syncSched();
    }
    function quickDows(kind) {
      sched.dows = kind === 'wd' ? [1, 2, 3, 4, 5] : [0, 1, 2, 3, 4, 5, 6];
      paintChips('scDows', sched.dows);
      syncSched();
    }
    function toggleDom(d) {
      const k = sched.doms.indexOf(d);
      if (k >= 0) { if (sched.doms.length === 1) return; sched.doms.splice(k, 1); } else sched.doms.push(d);
      sched.doms.sort((a, b) => a - b);
      paintChips('scDoms', sched.doms);
      const note = $('scDomNote');
      if (note) note.hidden = !(sched.doms[sched.doms.length - 1] > 28);
      syncSched();
    }
    function pickInterval(unitChanged) {
      const u = $('f_sc_unit').value;
      if (unitChanged && u !== sched.unit) { sched.every = INTERVAL_DEFAULT[u]; $('f_sc_every').innerHTML = intervalOptsHTML(u, sched.every); }
      else sched.every = +$('f_sc_every').value || INTERVAL_DEFAULT[u];
      sched.unit = u;
      syncSched();
    }

    /* ---- time wheel (iOS-alarm style, scroll-snap, no library) --------- */
    function wheelsHTML() {
      const h12 = sched.hour % 12 || 12, ap = sched.hour < 12 ? 0 : 1;
      const col = (id, vals, sel, fmt) => `<div class="wheel" id="${id}" data-sel="${sel}" onscroll="wheelScroll(this)"><ul>${vals.map((v, i) => `<li class="${i === sel ? 'sel' : ''}" onclick="wheelPick('${id}',${i})">${fmt(v)}</li>`).join('')}</ul></div>`;
      const hrs = Array.from({ length: 12 }, (_, i) => i + 1), mins = Array.from({ length: 60 }, (_, i) => i);
      return `<div class="wheels">${col('whH', hrs, h12 - 1, v => v)}<div class="wsep">:</div>${col('whM', mins, sched.min, v => String(v).padStart(2, '0'))}${col('whA', ['AM', 'PM'], ap, v => v)}<div class="wband"></div></div>`;
    }
    let _wheelT = null;
    function wheelScroll(el) { clearTimeout(_wheelT); _wheelT = setTimeout(() => wheelSet(el.id, Math.round(el.scrollTop / 44)), 130); }
    function wheelPick(id, i) { const el = $(id); if (el) el.scrollTo({ top: i * 44, behavior: 'smooth' }); wheelSet(id, i); }
    function wheelSet(id, i) {
      const el = $(id);
      if (!el) return;
      const items = el.querySelectorAll('li');
      i = Math.max(0, Math.min(i, items.length - 1));
      if (String(i) === el.dataset.sel) return;
      el.dataset.sel = String(i);
      items.forEach((li, k) => li.classList.toggle('sel', k === i));
      const h12 = +$('whH').dataset.sel + 1, ap = +$('whA').dataset.sel;
      sched.min = +$('whM').dataset.sel;
      sched.hour = (h12 % 12) + (ap ? 12 : 0);
      syncSched();
    }
    // scrollTop is inert while the pane is display:none, so re-seat the wheels
    // whenever the Automation tab (or the schedule block) becomes visible.
    function positionWheels() {
      ['whH', 'whM', 'whA'].forEach(id => { const el = $(id); if (el && el.offsetParent) el.scrollTop = (+el.dataset.sel || 0) * 44; });
    }
    function refreshSched() {
      // An enabled schedule with no cron is a broken spec the picker can repair
      // (the old text field would just fail validation at Deploy). Only this
      // case writes on render — a disabled or absent schedule is left alone.
      const on = $('f_sc_on') && $('f_sc_on').checked, el = $('f_sc_cron');
      if (on && el && !el.value.trim() && sched.mode !== 'custom') syncSched();
      positionWheels();
      renderSchedSummary();
      syncSchedAudience();
    }

    /* ---- cron <-> picker ----------------------------------------------- */
    function cronFromSched() {
      if (sched.mode === 'interval') {
        const n = sched.every;
        if (sched.unit === 'hours') return n === 1 ? '0 * * * *' : `0 */${n} * * *`;
        return n === 1 ? '* * * * *' : `*/${n} * * * *`;
      }
      const t = `${sched.min} ${sched.hour}`;
      if (sched.mode === 'weekly') return `${t} * * ${(sched.dows.length ? sched.dows : [1]).join(',')}`;
      if (sched.mode === 'monthly') return `${t} ${(sched.doms.length ? sched.doms : [1]).join(',')} * *`;
      return `${t} * * *`;
    }
    // Reverse of cronFromSched. Returns null for anything the picker can't hold
    // exactly — the caller drops to Custom rather than approximating.
    function schedFromCron(cron) {
      const p = String(cron || '').trim().split(/\s+/);
      if (p.length !== 5) return null;
      const one = f => /^\d+$/.test(f) ? +f : null;
      const list = f => /^\d+(,\d+)*$/.test(f) ? f.split(',').map(Number).filter((v, i, a) => a.indexOf(v) === i).sort((a, b) => a - b) : null;
      const step = (f, unit) => {
        const n = f === '*' ? 1 : (/^\*\/\d+$/.test(f) ? +f.slice(2) : null);
        return n && INTERVAL_STEPS[unit].indexOf(n) >= 0 ? n : null;
      };
      const mi = p[0], hh = p[1], dom = p[2], mon = p[3], dow = p[4];
      if (mon !== '*') return null;
      if (dom === '*' && dow === '*' && hh === '*') {
        const n = step(mi, 'minutes');
        if (n) return { mode: 'interval', unit: 'minutes', every: n };
        // "0 * * * *" is hourly, not a minute interval — fall through.
      }
      const M = one(mi);
      if (M === null || M > 59) return null;
      if (dom === '*' && dow === '*') {
        const H = one(hh);
        if (H !== null && H <= 23) return { mode: 'daily', hour: H, min: M };
        if (M === 0) { const n = step(hh, 'hours'); if (n) return { mode: 'interval', unit: 'hours', every: n }; }
        return null;
      }
      const H = one(hh);
      if (H === null || H > 23) return null;
      if (dom === '*') { const d = list(dow); return d && d[d.length - 1] <= 6 ? { mode: 'weekly', dows: d, hour: H, min: M } : null; }
      if (dow === '*') { const d = list(dom); return d && d[0] >= 1 && d[d.length - 1] <= 31 ? { mode: 'monthly', doms: d, hour: H, min: M } : null; }
      return null;
    }
    function syncSched() {
      if (sched.mode !== 'custom') { const el = $('f_sc_cron'); if (el) el.value = cronFromSched(); }
      renderSchedSummary();
    }

    /* ---- cron preview (server-authoritative, B10) ----------------------
       The hand-rolled croniter clone that used to live here is gone: next-run
       times and the min-interval gap now come from conductor via the local
       /api/cron-preview proxy (cli#78: client cron solvers drift from croniter,
       which is what actually fires). Offline / logged out / invalid cron → null
       and the "Next:" row + cost warning simply hide; describeCron and the raw
       expression need no time math and keep working everywhere. Responses are
       memoised per (cron, tz) and fetched 300ms after the last edit — the
       summary re-renders on every picker tick/keystroke. */
    const _cronPrev = { cache: {}, timer: null };
    const _cronKey = (cron, tz) => cron + '\u0000' + tz;
    function cronPreviewCached(cron, tz) {
      return _cronPrev.cache[_cronKey(cron, tz)] || null;
    }
    async function fetchCronPreview(cron, tz) {
      const key = _cronKey(cron, tz);
      if (key in _cronPrev.cache) return _cronPrev.cache[key];
      try {
        const r = await fetch('/api/cron-preview', {
          method: 'POST', headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ cron, timezone: tz }),
        });
        // 404 = proxy not registered (shouldn't happen); non-2xx / null body = degrade
        _cronPrev.cache[key] = r.ok ? await r.json() : null;
      } catch (e) { _cronPrev.cache[key] = null; }
      return _cronPrev.cache[key];
    }
    function kickCronPreview(cron, tz, rerender) {
      if (_cronKey(cron, tz) in _cronPrev.cache) return;
      clearTimeout(_cronPrev.timer);
      _cronPrev.timer = setTimeout(async () => { await fetchCronPreview(cron, tz); rerender(); }, 300);
    }

    /* ---- plain-language rendering -------------------------------------- */
    const ordinal = n => n + (n % 100 >= 11 && n % 100 <= 13 ? 'th' : ['th', 'st', 'nd', 'rd'][n % 10] || 'th');
    const clock12 = (h, m) => `${h % 12 || 12}:${String(m).padStart(2, '0')} ${h < 12 ? 'AM' : 'PM'}`;
    function describeCron(cron, tz) {
      const p = schedFromCron(cron), zone = tz ? ` · ${tz}` : '';
      if (!p) return `Runs on cron ${String(cron || '').trim()}${zone}`;
      if (p.mode === 'interval') return `Runs every ${p.every === 1 ? p.unit.slice(0, -1) : p.every + ' ' + p.unit}${zone}`;
      const at = clock12(p.hour, p.min);
      if (p.mode === 'daily') return `Runs every day at ${at}${zone}`;
      if (p.mode === 'weekly') {
        const key = p.dows.join(',');
        const when = key === '0,1,2,3,4,5,6' ? 'every day' : key === '1,2,3,4,5' ? 'every weekday'
          : key === '0,6' ? 'every Saturday and Sunday' : 'every ' + p.dows.map(d => DOW_NAME[d]).join(', ');
        return `Runs ${when} at ${at}${zone}`;
      }
      return `Runs on the ${p.doms.map(ordinal).join(', ')} of every month at ${at}${zone}`;
    }
    function fmtRun(d, tz) {
      try { return new Intl.DateTimeFormat('en-US', { timeZone: tz, weekday: 'short', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }).format(d); }
      catch (e) { return d.toISOString(); }
    }
    const fmtGap = s => s % 3600 === 0 && s >= 3600 ? `${s / 3600} hour${s === 3600 ? '' : 's'}` : `${Math.round(s / 60)} minute${s === 60 ? '' : 's'}`;
    function costWarnHTML(gap) {
      // 阈值判定在服务端（cron-preview 的 warning 字段）；此处只渲染文案
      if (gap === null || gap === undefined) return '';
      const per = Math.round(2592000 / gap).toLocaleString('en-US');
      return `<div class="flagbar"><span>⚠️ Fires as often as every ${fmtGap(gap)} — roughly ${per} runs a month, and every run consumes credits. Frequent schedules get expensive fast.</span></div>`;
    }
    function renderSchedSummary() {
      const box = $('scSum');
      if (!box) return;
      const on = $('f_sc_on') && $('f_sc_on').checked;
      const cron = ($('f_sc_cron') ? $('f_sc_cron').value : '').trim();
      const tz = ($('f_sc_tz') && $('f_sc_tz').value) || 'UTC';
      if (!on || !cron) { box.hidden = true; box.innerHTML = ''; return; }
      const pv = cronPreviewCached(cron, tz);
      const runs = pv && pv.next_runs ? pv.next_runs : null;
      box.hidden = false;
      box.innerHTML = `<div class="ss-line">${esc(describeCron(cron, tz))}</div>`
        + (runs && runs.length ? `<div class="ss-next">Next: ${runs.slice(0, 3).map(s => esc(fmtRun(new Date(s), tz))).join(' · ')}</div>` : '')
        + `<div class="ss-cron">cron <span class="mono">${esc(cron)}</span></div>`
        + (pv && pv.warning ? costWarnHTML(pv.min_interval_seconds) : '');
      kickCronPreview(cron, tz, renderSchedSummary);
    }
    function switchTab(name, btn) {
      document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
      btn.classList.add('active');
      document.querySelectorAll('.tabpane').forEach(p => p.hidden = p.dataset.pane !== name);
      if (name === 'automation') refreshSched();
    }
    function skillItem(c, i) {
      // onchange：内建 skill 在备份运行时不可用，勾选状态直接决定 Step 3 横幅（conductor #372）
      if (c.builtin) return `<label class="item"><input type="checkbox" id="skill-${i}" ${c.checked ? 'checked' : ''} onchange="syncHarnessGates()"><div><div class="nm">${esc(c.name)} <span class="tag ink">builtin</span></div></div></label>`;
      const sc = c.scope === 'user' ? '<span class="tag user">global</span>' : '<span class="tag proj">project</span>';
      const hb = c.harness ? `<span class="tag">${esc(hLabel(c.harness))}</span>` : '';
      const og = c.origin && c.origin !== 'repo-native' ? `<span class="tag">${esc(c.origin)}</span>` : '';
      return `<label class="item"><input type="checkbox" id="skill-${i}" ${c.checked ? 'checked' : ''}><div><div class="nm">${esc(c.name)} ${sc}${hb}${og}</div>${c.description ? `<div class="ds">${esc(c.description)}</div>` : ''}</div></label>`;
    }
    function skillGroups() {
      const cs = data.skill_candidates || [];
      if (!cs.length) return '<p class="empty">No skills detected in this project.</p>';
      // Within each scope, cluster by harness (stable sort) so multi-harness lists read cleanly.
      const idx = cs.map((c, i) => i);
      const proj = [], user = [], bi = [];
      idx.sort((a, b) => (cs[a].harness || '').localeCompare(cs[b].harness || ''));
      idx.forEach(i => { const c = cs[i], h = skillItem(c, i); c.builtin ? bi.push(h) : (c.scope === 'user' ? user.push(h) : proj.push(h)); });
      return (proj.length ? `<div class="grp"><h3>Current directory</h3>${proj.join('')}</div>` : '')
        + (user.length ? `<div class="grp"><h3>User-level (global)</h3>${user.join('')}</div>` : '')
        + (bi.length ? `<div class="grp"><h3>Inferred builtin</h3>${bi.join('')}</div>` : '');
    }
    function mcpItem(c, i) {
      const sc = c.scope === 'user' ? '<span class="tag user">global</span>' : '<span class="tag proj">project</span>';
      const hb = c.harness ? `<span class="tag">${esc(hLabel(c.harness))}</span>` : '';
      const wb = c.compatible ? '' : '<span class="tag bad">stdio · not deployable</span>';
      return `<label class="item"><input type="checkbox" id="mcp-${i}" ${c.checked ? 'checked' : ''}><div><div class="nm">${esc(c.name)} ${sc}${hb}<span class="tag">${esc(c.type)}</span>${wb}</div></div></label>`;
    }
    function mcpGroups() {
      const cs = data.mcp_candidates || [];
      if (!cs.length) return '<p class="empty">No MCP servers detected.</p>';
      const proj = [], user = [];
      cs.forEach((c, i) => { const h = mcpItem(c, i); c.scope === 'user' ? user.push(h) : proj.push(h); });
      return (proj.length ? `<div class="grp"><h3>Current directory</h3>${proj.join('')}</div>` : '')
        + (user.length ? `<div class="grp"><h3>User-level (global)</h3>${user.join('')}</div>` : '');
    }

    /* observe overlay (--observe): poll /api/observe, highlight what a real claude
       session actually used, and offer a one-tap narrow. Additive — default ticks
       stay as a safety net; nothing is auto-unticked unless the KOL asks. */
    let lastObserved = { skills: [], mcp_servers: [] };
    function observeBar() {
      if (!data.observe) return '';
      return `<div class="obsbar">
    <div class="obsmsg"><b>Observe mode.</b> In a second terminal, run <code>claude</code> in this project and walk a representative scenario — the skills &amp; MCP it actually uses get highlighted below.
      <span class="obscount" id="obsCount">Watching for activity…</span></div>
    <button type="button" class="act btn" onclick="narrowToObserved()">Keep only observed</button>
  </div>`;
    }
    function startObservePolling() {
      if (!data.observe) return;
      const poll = async () => { try { applyObserved(await (await fetch('/api/observe')).json()); } catch (e) { } };
      poll();
      setInterval(poll, 2000);
    }
    function applyObserved(o) {
      lastObserved = o || { skills: [], mcp_servers: [] };
      const usedSkills = new Set(lastObserved.skills || []), usedMCP = new Set(lastObserved.mcp_servers || []);
      (data.skill_candidates || []).forEach((c, i) => { if (!c.builtin) markUsed('skill-' + i, usedSkills.has(c.name)); });
      (data.mcp_candidates || []).forEach((c, i) => { markUsed('mcp-' + i, usedMCP.has(c.name)); });
      const el = $('obsCount');
      if (el) {
        const n = usedSkills.size, m = usedMCP.size;
        el.textContent = (n || m) ? `✓ observed ${n} skill${n === 1 ? '' : 's'} · ${m} MCP server${m === 1 ? '' : 's'}` : 'Watching for activity…';
      }
    }
    function markUsed(inputId, used) {
      const el = $(inputId); if (!el) return;
      const item = el.closest('.item'); if (!item) return;
      item.classList.toggle('used', used);
      let badge = item.querySelector('.tag.used');
      if (used && !badge) { const nm = item.querySelector('.nm'); if (nm) { badge = document.createElement('span'); badge.className = 'tag used'; badge.textContent = '✓ used'; nm.appendChild(badge); } }
      else if (!used && badge) { badge.remove(); }
    }
    function narrowToObserved() {
      const usedSkills = new Set(lastObserved.skills || []), usedMCP = new Set(lastObserved.mcp_servers || []);
      // builtins aren't activated via the Skill tool, so observe can't see them — leave them as-is.
      (data.skill_candidates || []).forEach((c, i) => { if (c.builtin) return; const el = $('skill-' + i); if (el) el.checked = usedSkills.has(c.name); });
      (data.mcp_candidates || []).forEach((c, i) => { const el = $('mcp-' + i); if (el) el.checked = usedMCP.has(c.name); });
      setStatus('Narrowed to observed skills/MCP — review, then deploy.');
    }

    /* SKILL.md frontmatter validation (desktop) — a custom_local skill missing
       name/description in SKILL.md fails the deploy gate (PackageSkills), so flag
       it inline in the Skills step with a one-tap fix instead of letting the KOL
       hit the wall at deploy. Additive overlay, same shape as the observe layer. */
    let skillFlags = {};
    async function validateSkills() {
      if (!data.desktop) return;
      let res;
      try { res = await (await fetch('/api/skill-validate', { method: 'POST' })).json(); }
      catch (e) { return; }
      if (!Array.isArray(res)) return;
      skillFlags = {};
      let nBad = 0;
      res.forEach(v => { skillFlags[v.path] = v; if (!v.has_name || !v.has_description) nBad++; });
      applySkillFlags();
      updateFlagBar(nBad);
    }
    function applySkillFlags() {
      (data.skill_candidates || []).forEach((c, i) => {
        if (c.builtin || !c.path) return;
        renderSkillFlag(i, c, skillFlags[c.path]);
      });
    }
    function renderSkillFlag(i, c, v) {
      const el = $('skill-' + i); if (!el) return;
      const col = el.nextElementSibling, item = el.closest('.item'); if (!col || !item) return;
      const prev = col.querySelector('.skill-flag'); if (prev) prev.remove();
      item.classList.remove('flag');
      if (!v || (v.has_name && v.has_description)) return; // complete — nothing to flag
      item.classList.add('flag');
      const missing = [];
      if (!v.has_name) missing.push('name');
      if (!v.has_description) missing.push('description');
      const box = document.createElement('div');
      box.className = 'skill-flag';
      box.onclick = (e) => { e.preventDefault(); e.stopPropagation(); }; // the flag area shouldn't toggle the skill checkbox
      const msg = document.createElement('div');
      msg.textContent = '⚠ SKILL.md is missing ' + missing.join(' & ') + " — the model can't trigger this skill until it's set, and deploy will reject it.";
      box.appendChild(msg);
      const row = document.createElement('div');
      row.className = 'fix-row';
      if (!v.has_name) {
        const b = document.createElement('button');
        b.type = 'button'; b.textContent = 'Use folder name "' + v.dir_name + '"';
        b.onclick = (e) => { e.stopPropagation(); e.preventDefault(); fixSkill(c.path, { name: v.dir_name }); };
        row.appendChild(b);
      }
      if (!v.has_description) {
        const inp = document.createElement('input');
        inp.type = 'text'; inp.placeholder = 'One line: when should the agent use this skill?';
        inp.onclick = (e) => e.stopPropagation();
        const b = document.createElement('button');
        b.type = 'button'; b.textContent = 'Save description';
        b.onclick = (e) => { e.stopPropagation(); e.preventDefault(); fixSkill(c.path, { description: inp.value }); };
        row.appendChild(inp); row.appendChild(b);
      }
      box.appendChild(row);
      col.appendChild(box);
    }
    async function fixSkill(path, fields) {
      if (fields.description !== undefined && !fields.description.trim()) { setStatus('Enter a description first.'); return; }
      try {
        const r = await fetch('/api/skill-fix', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(Object.assign({ path }, fields)) });
        const j = await r.json();
        if (j.error) { setStatus('Fix failed: ' + j.error); return; }
        setStatus('SKILL.md updated.');
        validateSkills(); // re-check — the fixed skill should clear its flag
      } catch (e) { setStatus('Fix failed: ' + (e.message || e)); }
    }
    function skillFlagBar() { return '<div class="flagbar" id="skillFlagBar" hidden></div>'; }
    function updateFlagBar(nBad) {
      const bar = $('skillFlagBar'); if (!bar) return;
      bar.innerHTML = '';
      if (!nBad) { bar.hidden = true; return; }
      bar.hidden = false;
      const span = document.createElement('span');
      span.textContent = '⚠ ' + nBad + ' skill' + (nBad === 1 ? '' : 's') + ' need a complete SKILL.md before deploy.';
      const btn = document.createElement('button');
      btn.type = 'button'; btn.className = 'act btn'; btn.textContent = 'Re-check';
      btn.onclick = () => validateSkills();
      bar.appendChild(span); bar.appendChild(btn);
    }

    /* vault / secrets */
    function vaultPaneHTML() {
      const rows = vault.length ? vault.map((c, i) => {
        const ck = c.checked ? 'checked' : '';
        if (c.readonly) {
          // scanned credential: single tick like a skill/mcp candidate + read-only name
          // and required/optional tag. Untick to leave it out; no required box or ×.
          const tag = c.required ? '<span class="tag ink">required</span>' : '<span class="tag">optional</span>';
          return `<label class="item"><input type="checkbox" id="secret-${i}" ${ck}><div><div class="nm"><span class="mono">${esc(c.name)}</span> ${tag}</div>${c.purpose ? `<div class="ds">${esc(c.purpose)}</div>` : ''}</div></label>`;
        }
        // manual credential (+ Add credential): still needs inputs to fill; tick + delete.
        return `<div class="vrow">
       <input type="checkbox" class="vtick" id="secret-${i}" ${ck} title="include">
       <input type="text" class="vn" id="v-name-${i}" value="${esc(c.name)}" placeholder="ENV_VAR_NAME">
       <input type="text" class="vp" id="v-purpose-${i}" value="${esc(c.purpose)}" placeholder="what it's for">
       <button type="button" class="vdel" title="remove" onclick="delVault(${i})">×</button>
     </div>`;
      }).join('')
        : '<p class="empty">No credentials detected. Add one if this agent needs API keys or secrets from its subscribers.</p>';
      return `<p class="card-note">Credentials each subscriber provides during onboarding — tick the ones this agent needs; values are never entered or stored here.</p><div class="field-err" id="err_vault"></div>${rows}<button type="button" class="addv" onclick="addVault()">+ Add credential</button>`;
    }
    function syncVault() { vault.forEach((c, i) => { const t = $('secret-' + i); if (t) c.checked = t.checked; if (!c.readonly && $('v-name-' + i)) { c.name = $('v-name-' + i).value.trim(); c.purpose = $('v-purpose-' + i).value.trim(); } }); }
    function refreshVaultPane() { const p = document.querySelector('[data-pane="vault"] .card'); if (p) p.innerHTML = vaultPaneHTML(); if ($('vaultCnt')) $('vaultCnt').textContent = vault.length; }
    function addVault() { syncVault(); vault.push({ name: '', purpose: '', required: true, checked: true, readonly: false }); refreshVaultPane(); }
    function delVault(i) { syncVault(); vault.splice(i, 1); refreshVaultPane(); }

    /* Step 4 · Review (no reasoning dump — confidence lives inline on each step) */
    function panelReview() {
      return `<section class="panel" data-step="3">
    <p class="eyebrow">Step 4 of 4 · Review &amp; Deploy</p>
    <h2>Review and ship</h2>
    <p class="sub">Confirm everything, then deploy or save as a draft.</p>
    <div class="card" style="position:relative">
      <div id="reviewAvatar" class="rev-av-corner"></div>
      <dl class="sum" id="reviewSum"></dl>
    </div>
  </section>`;
    }
    function fillReview() {
      collect();
      const m = spec.metadata;
      const skillNames = (data.skill_candidates || []).filter((c, i) => { const e = $('skill-' + i); return e && e.checked; }).map(c => c.name);
      const mcpNames = (data.mcp_candidates || []).filter((c, i) => { const e = $('mcp-' + i); return e && e.checked; }).map(c => c.name);
      const secretNames = vault.filter(c => c.name && c.checked).map(c => c.name);
      const chips = (arr, mono) => arr.length ? arr.map(n => `<span class="rtag${mono ? ' mono' : ''}">${esc(n)}</span>`).join('') : '<span class="empty">none</span>';
      const c = palette[m.theme_color] || '#1b365d';
      const sp = (spec.persona.system_prompt || '').trim();
      const spHTML = sp ? `<div class="rev-prompt">${esc(sp)}</div>` : '<span class="empty">—</span>';
      const av = $('reviewAvatar');
      if (av) { av.style.background = c; av.innerHTML = avatarInner(m.avatar || ''); }
      $('reviewSum').innerHTML = `
    <dt>Name</dt><dd>${esc(m.name) || '<span class="empty">unnamed</span>'}</dd>
    <dt>Display name</dt><dd>${esc(m.display_name) || '<span class="empty">—</span>'}</dd>
    <dt>Description</dt><dd>${esc(m.description) || '<span class="empty">—</span>'}</dd>
    <dt>Category</dt><dd>${esc(m.category) || '—'}</dd>
    <dt>Language</dt><dd>${m.language === 'zh' ? '中文 Chinese' : m.language === 'en' ? 'English' : m.language ? esc(m.language) : 'Auto · follows the user'}</dd>
    <dt>Visibility</dt><dd>${esc(m.visibility || 'private')}</dd>
    <dt>Model</dt><dd>${reviewModelHTML()}</dd>
    <dt>Runtime</dt><dd>${esc(HARNESS_LABEL[currentHarness()] || currentHarness())}${isOpenAI() && tickedBuiltinSkills().length ? '<div class="rev-sub"><span class="rev-warn">⚠️ built-in Anthropic skills are skipped on this runtime (your own skills run)</span></div>' : ''}</dd>
    <dt>System prompt</dt><dd>${spHTML}</dd>
    <dt>Skills (${skillNames.length})</dt><dd class="chips">${chips(skillNames)}</dd>
    <dt>MCP servers (${mcpNames.length})</dt><dd class="chips">${chips(mcpNames)}</dd>
    <dt>Secrets (${secretNames.length})</dt><dd class="chips">${chips(secretNames, true)}</dd>
    <dt>Outcomes</dt><dd>${spec.outcomes && spec.outcomes.enabled !== false ? `rubric · max ${spec.outcomes.max_iterations || 3} grader iterations` : '<span class="empty">off</span>'}</dd>
    <dt>Schedule</dt><dd>${reviewScheduleHTML()}</dd>
    <dt>Memory &amp; knowledge</dt><dd>${reviewKnowledgeHTML()}</dd>`;
    }
    function reviewKnowledgeHTML() {
      const m = spec.memory || {}, w = spec.wiki || {}, on = [];
      if (m.inject_user_profile) on.push('user profile');
      if (m.inject_agent_profile) on.push('agent profile');
      if (w.enabled) on.push(w.evolution ? 'wiki + evolution' : 'wiki');
      return on.length
        ? `${esc(on.join(' · '))} <span class="empty">· applied on every deploy</span>`
        : '<span class="empty">off</span>';
    }
    function reviewScheduleHTML() {
      const s = spec.schedule;
      if (!s || s.enabled === false || !s.cron) return '<span class="empty">off</span>';
      const tz = s.timezone || 'UTC', pv = cronPreviewCached(s.cron, tz);
      const runs = pv && pv.next_runs && pv.next_runs.length ? pv.next_runs : null;
      kickCronPreview(s.cron, tz, fillReview);
      return `${s.mode === 'broadcast' ? 'Broadcast · ' : ''}${esc(describeCron(s.cron, tz))}${s.notify_channel ? ' → ' + esc(s.notify_channel) : ''}`
        + `<div class="rev-sub">${runs ? 'Next ' + esc(fmtRun(new Date(runs[0]), tz)) + ' · ' : ''}<span class="mono">${esc(s.cron)}</span>`
        + `${pv && pv.warning ? ' · <span class="rev-warn">⚠️ frequent — cost risk</span>' : ''}</div>`;
    }

    /* validation — required fields + slug name format; gates Next + Deploy. Draft stays
       lenient (name only). Mirrors conductor's required set (name/model_class) plus the
       product-required fields (display_name/description/system_prompt/secret name). */
    const NAME_RE = /^[a-z0-9]+(-[a-z0-9]+)*$/;
    const FIELD_INPUT = { name: 'f_name', display: 'f_display', desc: 'f_desc', prompt: 'f_prompt', oc_rubric: 'f_oc_rubric', sc_cron: 'f_sc_cron', sc_task: 'f_sc_task' };
    function validateStep(n) {
      collect();
      const m = spec.metadata, p = spec.persona, errs = [];
      if (n === 0) {
        const name = (m.name || '').trim();
        if (!name) errs.push({ id: 'name', msg: 'Agent name is required' });
        else if (!NAME_RE.test(name)) errs.push({ id: 'name', msg: 'Use lowercase letters, numbers and hyphens, e.g. my-agent' });
        else if (name.length > 64) errs.push({ id: 'name', msg: 'Keep the name under 64 characters' });
        if (!(m.display_name || '').trim()) errs.push({ id: 'display', msg: 'Display name is required' });
        if (!(m.description || '').trim()) errs.push({ id: 'desc', msg: 'Description is required' });
      } else if (n === 1) {
        if (models().length ? !modelById(currentModelId()) : !(p.model_class || '').trim()) errs.push({ id: 'model', msg: 'Pick a model' });
        if (!(p.system_prompt || '').trim()) errs.push({ id: 'prompt', msg: 'System prompt is required' });
      } else if (n === 2) {
        syncVault();
        vault.forEach((c, i) => { if (!(c.name || '').trim() && (c.purpose || '').trim()) errs.push({ id: 'v-name-' + i, vault: true }); });
        // Automation (#303): only validate what's switched on.
        if ($('f_oc_on') && $('f_oc_on').checked && !($('f_oc_rubric').value || '').trim())
          errs.push({ id: 'oc_rubric', msg: 'A rubric is required when outcomes are enabled', automation: true });
        if ($('f_sc_on') && $('f_sc_on').checked) {
          const cron = ($('f_sc_cron').value || '').trim();
          if (!cron) errs.push({ id: 'sc_cron', msg: 'A cron expression is required', automation: true });
          else if (cron.split(/\s+/).length !== 5) errs.push({ id: 'sc_cron', msg: 'Cron needs exactly 5 fields, e.g. 0 9 * * 1', automation: true });
          if (!($('f_sc_task').value || '').trim()) errs.push({ id: 'sc_task', msg: 'A task instruction is required', automation: true });
        }
      }
      return errs;
    }
    function validateAll() {
      for (let n = 0; n < 3; n++) { const e = validateStep(n); if (e.length) return { step: n, errs: e }; }
      return null;
    }
    function clearErrors() {
      document.querySelectorAll('.field-err').forEach(e => e.textContent = '');
      document.querySelectorAll('.invalid').forEach(e => e.classList.remove('invalid'));
    }
    function showErrors(errs) {
      clearErrors();
      let first = null, vaultErr = false, automationErr = false;
      errs.forEach(e => {
        if (e.automation) automationErr = true;
        if (e.vault) { vaultErr = true; const inp = $(e.id); if (inp) { inp.classList.add('invalid'); if (!first) first = inp; } return; }
        const box = $('err_' + e.id); if (box) box.textContent = e.msg;
        const inp = FIELD_INPUT[e.id] && $(FIELD_INPUT[e.id]);
        if (inp) { inp.classList.add('invalid'); if (!first) first = inp; }
        else if (box && !first) first = box;
      });
      if (vaultErr) {
        const vb = $('err_vault'); if (vb) vb.textContent = 'Every secret needs a name (or remove the empty row)';
        const sb = document.querySelectorAll('.tab')[2]; if (sb) switchTab('vault', sb);
      } else if (automationErr) {
        const ab = document.querySelectorAll('.tab')[3]; if (ab) switchTab('automation', ab);
      }
      setStatus('✗ Please fix the highlighted fields');
      if (first) first.scrollIntoView({ block: 'center', behavior: 'smooth' });
    }

    /* navigation */
    function go(n) {
      if (n < 0 || n >= STEPS.length) return;
      collect();
      if (n > step) { const errs = validateStep(step); if (errs.length) { showErrors(errs); return; } }
      clearErrors();
      step = n;
      document.querySelectorAll('.panel').forEach(p => { const on = +p.dataset.step === n; p.classList.toggle('show', on); p.hidden = !on; });
      document.querySelectorAll('.step-nav li').forEach((li, i) => { li.classList.toggle('active', i === n); li.classList.toggle('done', i < n); });
      if (n === 3) fillReview();
      const last = n === STEPS.length - 1;
      $('btnBack').hidden = n === 0; $('btnNext').hidden = last; $('btnDeploy').hidden = !last; $('btnDone').hidden = !last;
      setStatus(''); $('panels').parentElement.scrollTop = 0;
    }
    function pickTheme(tok) {
      spec.metadata.theme_color = tok; applyAccent(tok);
      document.querySelectorAll('.swatches .sw').forEach(s => s.classList.remove('sel'));
      if (event && event.target) event.target.classList.add('sel');
      renderAvatar();
    }
    function applyAccent(tok) {
      const hex = palette[tok] || '#1b365d';
      document.documentElement.style.setProperty('--accent', hex);
      document.documentElement.style.setProperty('--accent-soft', hex + '22');
    }
    /* avatar: ""=initial / "icon:<name>"=lucide+theme_color circle. 颜色复用 theme_color. */
    function avatarInner(value) {
      const m = spec.metadata;
      if (value && value.indexOf('icon:') === 0) {
        const ic = (data.icons || []).find(i => i.name === value.slice(5));
        if (ic) return `<svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width:55%;height:55%">${ic.svg}</svg>`;
      }
      const nm = m.display_name || m.name || '·';
      const ch = (nm.match(/\p{L}/u) || ['·'])[0].toUpperCase();
      return `<span style="color:#fff;font-weight:600;font-size:20px">${esc(ch)}</span>`;
    }
    function renderAvatar() {
      const pv = $('avPreview'); if (!pv) return;
      pv.style.background = palette[spec.metadata.theme_color] || '#1b365d';
      pv.innerHTML = avatarInner(spec.metadata.avatar || '');
    }
    function pickAvatar(value) {
      spec.metadata.avatar = value;
      document.querySelectorAll('#avGrid .av-cell').forEach(c => c.classList.remove('sel'));
      if (event && event.currentTarget) event.currentTarget.classList.add('sel');
      renderAvatar();
    }
    function renderIconGrid() {
      const g = $('avGrid'); if (!g) return;
      const cur = spec.metadata.avatar || '';
      let h = `<button type="button" class="av-cell${cur === '' ? ' sel' : ''}" title="Default (initial)" onclick="pickAvatar('')">Aa</button>`;
      h += (data.icons || []).map(ic => {
        const v = 'icon:' + ic.name;
        return `<button type="button" class="av-cell${cur === v ? ' sel' : ''}" title="${esc(ic.name)}" onclick="pickAvatar('${v}')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width:18px;height:18px">${ic.svg}</svg></button>`;
      }).join('');
      g.innerHTML = h;
    }
    function collect() {
      const m = spec.metadata, p = spec.persona;
      if ($('f_name')) m.name = $('f_name').value.trim();
      if ($('f_display')) m.display_name = $('f_display').value.trim();
      if ($('f_desc')) m.description = $('f_desc').value.trim();
      if ($('f_cat')) m.category = $('f_cat').value;
      if ($('f_lang')) m.language = $('f_lang').value; // '' = universal (omitempty drops it from yaml)
      if ($('f_prompt')) p.system_prompt = $('f_prompt').value;
      if (models().length) {
        // Whitelist mode: the dropdown's pick is the pin (provider/harness from the
        // catalog entry, never hardcoded) — see applyModel().
        const sel = $('modelSel');
        applyModel(sel ? sel.value : currentModelId());
      } else {
        // Legacy tier mode: fill model_preferences[0] from the fetched catalog.
        // Offline fallback has no model_id → leave empty; conductor resolves the
        // concrete id from model_class at deploy time.
        const mc = modelEntry(p.model_class);
        p.model_preferences = (mc && mc.model_id) ? [{ provider: 'anthropic', id: mc.model_id }] : [];
      }
      spec.skills = [];
      (data.skill_candidates || []).forEach((c, i) => {
        const el = $('skill-' + i); if (!el || !el.checked) return;
        if (c.builtin) spec.skills.push({ type: 'builtin', provider: 'anthropic', id: c.builtin_id });
        else { const s = { type: 'custom_local', path: c.path }; if (c.scope === 'user') s.scope = 'user'; spec.skills.push(s); }
      });
      spec.mcp_servers = [];
      (data.mcp_candidates || []).forEach((c, i) => {
        const el = $('mcp-' + i); if (!el || !el.checked) return;
        const s = { name: c.name, type: c.type }; if (c.url) s.url = c.url; if (c.command) s.command = c.command; spec.mcp_servers.push(s);
      });
      syncVault();
      const pickedVault = vault.filter(c => c.name && c.checked);
      spec.vault_hints = {
        required_credentials: pickedVault.filter(c => c.required).map(c => ({ name: c.name, purpose: c.purpose, required: true })),
        optional_credentials: pickedVault.filter(c => !c.required).map(c => ({ name: c.name, purpose: c.purpose, required: false })),
      };
      // Automation (#303): only persist a block when it carries content —
      // an untouched pane leaves the yaml free of outcomes/schedule keys.
      if ($('f_oc_on')) {
        const on = $('f_oc_on').checked, rubric = ($('f_oc_rubric') ? $('f_oc_rubric').value : '').trim();
        if (on || rubric) {
          const o = { enabled: on, rubric: rubric };
          const it = parseInt($('f_oc_iters') && $('f_oc_iters').value, 10);
          if (it >= 1 && it <= 5) o.max_iterations = it;
          spec.outcomes = o;
        } else delete spec.outcomes;
      }
      if ($('f_sc_on')) {
        const on = $('f_sc_on').checked, cron = ($('f_sc_cron') ? $('f_sc_cron').value : '').trim();
        const task = ($('f_sc_task') ? $('f_sc_task').value : '').trim();
        if (on || cron || task) {
          const s = { enabled: on, cron: cron, task: task };
          // mode: personal is the default — omit it so an untouched spec stays byte-exact.
          if (schedAudience() === 'broadcast') s.mode = 'broadcast';
          const tz = $('f_sc_tz') && $('f_sc_tz').value;
          if (tz && tz !== 'UTC') s.timezone = tz;
          const nc = $('f_sc_notify') && $('f_sc_notify').value;
          if (nc) s.notify_channel = nc;
          spec.schedule = s;
        } else delete spec.schedule;
      }
      // Memory injection + wiki switches: same discipline as automation —
      // only emit a block when something is actually on, so an untouched pane
      // never grows an empty block in the yaml. Values are first-deploy seeds;
      // conductor ignores them on redeploy.
      if ($('f_mem_user')) {
        const u = $('f_mem_user').checked, a = $('f_mem_agent').checked;
        const mem = spec.memory || {};
        if (u || a) { mem.inject_user_profile = u; mem.inject_agent_profile = a; }
        else { delete mem.inject_user_profile; delete mem.inject_agent_profile; }
        if (Object.keys(mem).length) spec.memory = mem; else delete spec.memory;
      }
      if ($('f_wiki_on')) {
        const on = $('f_wiki_on').checked, evo = $('f_wiki_evo').checked;
        if (on || evo) spec.wiki = { enabled: on, evolution: evo };
        else delete spec.wiki;
      }
      return spec;
    }
    let busy = false;
    async function submit(path) {
      if (busy) return;
      collect();
      if (path === '/api/save') {
        // draft stays lenient: only name (file + dedup key) must be present
        if (!(spec.metadata.name || '').trim()) { if (step !== 0) go(0); showErrors([{ id: 'name', msg: 'Agent name is required, even for a draft' }]); return; }
      } else {
        const bad = validateAll();
        if (bad) { if (step !== bad.step) go(bad.step); showErrors(bad.errs); return; }
      }
      busy = true; setStatus('Working…');
      const failTitle = path === '/api/deploy' ? "Couldn't deploy" : "Couldn't save";
      ['btnSaveDraft', 'btnBack', 'btnNext', 'btnDeploy', 'btnDone'].forEach(b => $(b).disabled = true);
      try {
        const r = await fetch(path, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(collect()) });
        const j = await r.json();
        if (j.error) showDialog({ kind: 'error', title: failTitle, msg: j.error, terminal: false });
        else if (j.status === 'saved') showDialog({ kind: 'success', title: 'Draft saved', msg: 'askdao-agent.yml saved. Keep editing or deploy when ready.', terminal: false });
        else if (j.status === 'deployed') { showDialog({ kind: 'success', title: 'Agent deployed', msg: j.message, link: j.agent_url, terminal: true, agentId: j.agent_id, warn: j.schedule_warning }); return; }
        else { showDialog({ kind: 'success', title: 'Saved', msg: j.message || 'askdao-agent.yml saved.', terminal: true }); return; }
      } catch (e) { showDialog({ kind: 'error', title: failTitle, msg: String(e), terminal: false }); }
      finally { busy = false;['btnSaveDraft', 'btnBack', 'btnNext', 'btnDeploy', 'btnDone'].forEach(b => $(b).disabled = false); }
    }
    function setStatus(s) { $('status').textContent = s; }
    /* unified result dialog — every Save/Deploy outcome surfaces here instead of the
       cramped footer status line. kind: success(green ✓) | error(red ✕). terminal=true
       means the server has exited (Deploy / Save & finish): show "close this tab" and
       no dismiss. terminal=false (Draft saved / any error): the server is still up, so
       offer a dismiss button (and click-outside) to return to editing. Deploy success
       carries the clickable agent page link (agent_url, always sent by the server). */
    let openLink = '', dialogTerminal = false, chatAgentID = '', chatSession = '';
    function showDialog({ kind, title, msg, link, terminal, agentId, warn }) {
      setStatus('');
      const ok = kind !== 'error';
      const chk = $('doneCheck'); chk.textContent = ok ? '✓' : '✕'; chk.classList.toggle('err', !ok);
      $('doneTitle').textContent = title;
      $('doneMsg').textContent = msg || '';
      // conductor's authoritative frequent-schedule cost notice — the client-side
      // estimate in the Automation tab is only the first check.
      $('doneWarn').hidden = !warn;
      $('doneWarn').firstElementChild.textContent = warn ? '⚠️ ' + warn : '';
      openLink = link || '';
      $('doneLink').hidden = !openLink;
      if (openLink) $('doneLinkOpen').href = openLink;
      // "terminal" (Deploy / Save & finish) means no-dismiss ONLY in the CLI browser,
      // where the local server exits and the page goes dead, so the hint is "close the
      // tab". The desktop app keeps the Wails server alive (server.go Handler never
      // exits on deploy/done), so always leave a way out: dismiss button, click-outside,
      // and a hint that points at the window instead of a tab.
      const noDismiss = terminal && !data.desktop;
      dialogTerminal = noDismiss;
      $('doneClose').hidden = !terminal;
      $('doneClose').textContent = data.desktop ? 'You can close the window, or keep editing.' : 'You can close this tab.';
      $('doneDismiss').hidden = noDismiss;
      $('doneDismiss').textContent = ok ? 'Continue' : 'Dismiss';
      // Desktop-only: after a successful deploy, offer to test-chat the just-deployed agent.
      chatAgentID = (data.desktop && ok && agentId) ? agentId : '';
      $('doneChatBtn').hidden = !chatAgentID;
      $('doneOverlay').hidden = false;
    }
    function closeDialog() { $('doneOverlay').hidden = true; setStatus(''); }
    function overlayClick(e) { if (e.target === $('doneOverlay') && !dialogTerminal) closeDialog(); }
    function copyOpenLink() {
      if (!openLink) return;
      navigator.clipboard.writeText(openLink).then(() => { const b = $('doneLinkCopy'), t = b.textContent; b.textContent = 'Copied ✓'; setTimeout(() => { b.textContent = t; }, 1500); });
    }

    // Desktop-only test chat: talk to the agent you just deployed. Streams
    // conductor's /chat SSE through the local /api/chat proxy — the same
    // fetch+getReader+TextDecoder pattern as ai-web, parsing raw `data:` frames
    // by .type (text_delta appends / done carries session_id for multi-turn /
    // error). The cli_ token never touches this page; the Go side holds it.
    // Messages render via textContent (never innerHTML) so agent output can't
    // inject markup.
    function openChat() {
      $('doneOverlay').hidden = true;
      chatSession = '';
      $('chatLog').innerHTML = '';
      addChatMsg('agent', 'Your agent is live. Send a message to test it.');
      $('chatOverlay').hidden = false;
      $('chatText').focus();
    }
    function closeChat() { $('chatOverlay').hidden = true; }
    function chatKey(e) { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendChat(e); } }
    function addChatMsg(role, text) {
      const el = document.createElement('div');
      el.className = 'chat-msg ' + role;
      el.textContent = text || '';
      const log = $('chatLog'); log.appendChild(el); log.scrollTop = log.scrollHeight;
      return el;
    }
    let chatBusy = false;
    async function sendChat(e) {
      if (e) e.preventDefault();
      if (chatBusy) return;
      const ta = $('chatText'), msg = ta.value.trim();
      if (!msg || !chatAgentID) return;
      addChatMsg('user', msg);
      ta.value = '';
      chatBusy = true; $('chatSend').disabled = true;
      const bubble = addChatMsg('agent', '…'); bubble.classList.add('typing');
      let full = '', started = false, clarifyPaused = false;
      try {
        const r = await fetch('/api/chat', {
          method: 'POST', headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ message: msg, agent_id: chatAgentID, session_id: chatSession || undefined }),
        });
        if (!r.ok || !r.body) throw new Error('chat failed (' + r.status + ')');
        const reader = r.body.getReader(), dec = new TextDecoder();
        let buf = '';
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buf += dec.decode(value, { stream: true });
          const lines = buf.split('\n'); buf = lines.pop() || '';
          for (const line of lines) {
            const t = line.trim();
            if (!t || t.startsWith(':') || !t.startsWith('data:')) continue;
            let ev; try { ev = JSON.parse(t.slice(t.indexOf(':') + 1).trim()); } catch { continue; }
            if (ev.type === 'text_delta' && ev.text) {
              if (!started) { started = true; bubble.classList.remove('typing'); bubble.textContent = ''; }
              full += ev.text; bubble.textContent = full;
              $('chatLog').scrollTop = $('chatLog').scrollHeight;
            } else if (ev.type === 'text_delta_replacement') {
              // Scaffolding rewrites the whole answer (conductor meta_skills): replace the
              // buffer, don't append — test chat should show the final answer subscribers
              // get, not the intermediate raw draft (matches ai-web fullText = event.text).
              started = true; bubble.classList.remove('typing');
              full = ev.text || ''; bubble.textContent = full;
              $('chatLog').scrollTop = $('chatLog').scrollHeight;
            } else if (ev.type === 'done') {
              chatSession = ev.sdk_session_id || ev.ov_session_id || chatSession;
              if (ev.awaiting_clarify) clarifyPaused = true;
            } else if (ev.type === 'error') {
              bubble.classList.remove('typing'); bubble.classList.add('err');
              bubble.textContent = 'Error: ' + (ev.message || 'unknown');
            }
          }
        }
        if (clarifyPaused && !started) {
          bubble.classList.remove('typing');
          bubble.textContent = "The agent is asking a clarifying question. Interactive questions aren't supported in test chat yet — try it in the live product to answer.";
        } else if (!started && !bubble.classList.contains('err')) {
          bubble.classList.remove('typing'); bubble.textContent = '(no response)';
        }
      } catch (err) {
        bubble.classList.remove('typing'); bubble.classList.add('err');
        bubble.textContent = 'Error: ' + (err.message || err);
      } finally {
        chatBusy = false; $('chatSend').disabled = false; $('chatText').focus();
      }
    }

    // AI assistant (desktop) — the build helper. Streams /api/assistant; the Go side
    // forwards to the official Studio assistant agent, resolving its id (agent_id
    // never touches this page). Same SSE frame parsing as sendChat, plus an
    // "unavailable" frame (not logged in / no official assistant configured) →
    // degrade to a static message instead of a wrong-agent chat. textContent only.
    let asstSession = '', asstBusy = false;
    function openAssistant() {
      $('assistantDrawer').classList.add('open');
      $('assistantDrawer').setAttribute('aria-hidden', 'false');
      if (!$('asstLog').childElementCount) addAsstMsg('agent', "Hi! I can help you build this agent — explain a field, suggest a system prompt, or draft a SKILL.md description. What would you like?");
      $('asstText').focus();
    }
    function closeAssistant() {
      $('assistantDrawer').classList.remove('open');
      $('assistantDrawer').setAttribute('aria-hidden', 'true');
    }
    function assistantKey(e) { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendAssistant(e); } }
    function addAsstMsg(role, text) {
      const el = document.createElement('div');
      el.className = 'chat-msg ' + role;
      el.textContent = text || '';
      const log = $('asstLog'); log.appendChild(el); log.scrollTop = log.scrollHeight;
      return el;
    }
    async function sendAssistant(e) {
      if (e) e.preventDefault();
      if (asstBusy) return;
      const ta = $('asstText'), msg = ta.value.trim();
      if (!msg) return;
      addAsstMsg('user', msg);
      ta.value = '';
      asstBusy = true; $('asstSend').disabled = true;
      const bubble = addAsstMsg('agent', '…'); bubble.classList.add('typing');
      let full = '', started = false;
      try {
        const r = await fetch('/api/assistant', {
          method: 'POST', headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ message: msg, session_id: asstSession || undefined }),
        });
        if (!r.ok || !r.body) throw new Error('assistant failed (' + r.status + ')');
        const reader = r.body.getReader(), dec = new TextDecoder();
        let buf = '';
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buf += dec.decode(value, { stream: true });
          const lines = buf.split('\n'); buf = lines.pop() || '';
          for (const line of lines) {
            const t = line.trim();
            if (!t || t.startsWith(':') || !t.startsWith('data:')) continue;
            let ev; try { ev = JSON.parse(t.slice(t.indexOf(':') + 1).trim()); } catch { continue; }
            if (ev.type === 'text_delta' && ev.text) {
              if (!started) { started = true; bubble.classList.remove('typing'); bubble.textContent = ''; }
              full += ev.text; bubble.textContent = full;
              $('asstLog').scrollTop = $('asstLog').scrollHeight;
            } else if (ev.type === 'text_delta_replacement') {
              started = true; bubble.classList.remove('typing');
              full = ev.text || ''; bubble.textContent = full;
              $('asstLog').scrollTop = $('asstLog').scrollHeight;
            } else if (ev.type === 'done') {
              asstSession = ev.sdk_session_id || ev.ov_session_id || asstSession;
            } else if (ev.type === 'unavailable') {
              started = true; bubble.classList.remove('typing');
              bubble.textContent = ev.message || "The assistant isn't available right now.";
            } else if (ev.type === 'error') {
              started = true; bubble.classList.remove('typing'); bubble.classList.add('err');
              bubble.textContent = 'Error: ' + (ev.message || 'unknown');
            }
          }
        }
        if (!started && !bubble.classList.contains('err')) { bubble.classList.remove('typing'); bubble.textContent = '(no response)'; }
      } catch (err) {
        bubble.classList.remove('typing'); bubble.classList.add('err');
        bubble.textContent = 'Error: ' + (err.message || err);
      } finally {
        asstBusy = false; $('asstSend').disabled = false; $('asstText').focus();
      }
    }

    // Desktop: the webview ignores target=_blank / window.open, so route external-
    // link clicks (the agent page, any future external link) through the Go side —
    // POST /api/open-external opens them in the real browser. Registered once;
    // guarded by data.desktop at click time. CLI keeps native target=_blank.
    document.addEventListener('click', (e) => {
      if (!data || !data.desktop) return;
      const a = e.target.closest && e.target.closest('a[target="_blank"]');
      if (!a || !a.getAttribute('href')) return;
      e.preventDefault();
      fetch('/api/open-external', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ url: a.href }) });
    });

    // confidence tooltip — single fixed node near <body>, positioned by JS; immune
    // to card stacking-contexts and the .wrap scroll-overflow that clipped the old
    // CSS pseudo-element version. Flips above the badge when there's no room below.
    document.addEventListener('mouseover', e => {
      const c = e.target.closest && e.target.closest('.conf'); if (!c) return;
      const t = $('tip'); t.textContent = c.getAttribute('data-tip') || '';
      const r = c.getBoundingClientRect();
      t.style.left = r.left + 'px'; t.style.top = (r.bottom + 8) + 'px'; t.classList.add('show');
      requestAnimationFrame(() => {
        const tr = t.getBoundingClientRect();
        if (tr.bottom > innerHeight - 12) t.style.top = Math.max(8, r.top - tr.height - 8) + 'px';
        if (tr.right > innerWidth - 12) t.style.left = Math.max(8, innerWidth - tr.width - 12) + 'px';
      });
    });
    document.addEventListener('mouseout', e => { if (e.target.closest && e.target.closest('.conf')) $('tip').classList.remove('show'); });

    load();
