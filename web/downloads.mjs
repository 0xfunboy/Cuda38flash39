// Native model acquisition UI. Metadata browsing and plan creation never start
// transfers. The server owns durable progress; browser polling is visibility-bound.
export function downloadJobActions(status) {
  if (status === 'PLANNED') return ['start', 'cancel'];
  if (['PAUSED', 'FAILED', 'CANCELLED'].includes(status)) return ['resume'];
  if (['RUNNING', 'VERIFYING'].includes(status)) return ['pause', 'cancel'];
  return [];
}

export function downloadProgress(job) {
  const complete = Number.isFinite(job?.bytes) && job.bytes >= 0 ? job.bytes : 0;
  const total = Number.isFinite(job?.total_bytes) && job.total_bytes >= 0 ? job.total_bytes : null;
  return { bytes: complete, total, fraction: total > 0 ? Math.min(1, complete / total) : job?.status === 'COMPLETE' ? 1 : 0 };
}

export function initDownloads({ request, notice, bytes, seconds, isAuthenticated = () => true }) {
  const panel = document.getElementById('panel-models');
  const state = { listing: null, busy: false, refreshing: false, timer: null, jobs: [], connected: false, authEpoch: 0 };
  const el = (tag, text = '', className = '') => { const n = document.createElement(tag); if (text) n.textContent = text; if (className) n.className = className; return n; };
  const button = (text, action) => { const b = el('button', text, 'button quiet'); b.type = 'button'; if (action) b.addEventListener('click', action); return b; };
  const field = (form, text, node) => { const label = el('label', text); label.htmlFor = node.id; form.append(label, node); return node; };
  const select = (id, choices) => { const s = el('select'); s.id = id; for (const [value, text] of choices) { const option = el('option', text); option.value = value; s.append(option); } return s; };
  const input = (id, placeholder, required = false) => { const n = el('input'); n.id = id; n.placeholder = placeholder; n.required = required; n.autocomplete = 'off'; return n; };
  const section = el('section', '', 'surface download-section'); section.id = 'model-downloads'; section.setAttribute('aria-label', 'Model downloads');
  section.append(el('h2', 'Download models'));
  const explanation = el('p', 'Search public Hugging Face and ModelScope repositories, select files, then review a plan before starting. GGUF, safetensors and other files are acquired without loading or converting the model.', 'small muted');
  const sourceNote = el('p', 'vLLM is an inference runtime; VLM means a vision-language model. Neither is a model hosting service. A downloaded model is not automatically compatible with this runtime.', 'small muted');
  const storage = el('p', 'Sign in to search public sources and inspect the managed destination and preserved download jobs.', 'small muted'); storage.id = 'download-storage';
  section.append(explanation, sourceNote, storage);

  const forms = el('div', '', 'download-forms');
  const searchForm = el('form', '', 'download-form'); searchForm.id = 'download-search-form'; searchForm.append(el('h3', 'Search repositories'));
  const source = field(searchForm, 'Source', select('download-source', [['all', 'Hugging Face + ModelScope'], ['huggingface', 'Hugging Face'], ['modelscope', 'ModelScope']]));
  const query = field(searchForm, 'Model or organization', input('download-query', 'Qwen, GLM, GGUF, vision…', true)); query.minLength = 2; query.maxLength = 160;
  const searchButton = button('Search'); searchButton.type = 'submit'; searchForm.append(searchButton);
  const urlForm = el('form', '', 'download-form'); urlForm.id = 'download-url-form'; urlForm.append(el('h3', 'Direct public URL'));
  const direct = field(urlForm, 'HTTPS file URL', input('download-url', 'https://host/model.gguf', true)); direct.type = 'url'; direct.maxLength = 8192;
  const hash = field(urlForm, 'Expected SHA256 (optional)', input('download-sha256', '64 lowercase hex characters')); hash.pattern = '[a-f0-9]{64}'; hash.maxLength = 64;
  urlForm.append(el('p', 'Public source files only: no source credentials, private hosts or signed input URLs. HaloClu sign-in is still required. Hugging Face file links resolve to an immutable commit; repository pages require file selection.', 'small muted'));
  const urlButton = button('Create download plan'); urlButton.type = 'submit'; urlForm.append(urlButton); forms.append(searchForm, urlForm); section.append(forms);
  const feedback = el('p', '', 'small'); feedback.id = 'download-feedback'; feedback.setAttribute('role', 'status'); section.append(feedback);
  const results = el('div', '', 'download-search-results'); results.id = 'download-search-results'; section.append(results);
  const fileSection = el('section', '', 'download-file-section'); fileSection.hidden = true; fileSection.append(el('h3', 'Select files'));
  const pin = el('p', '', 'small muted'); pin.id = 'download-pin';
  const filter = input('download-file-filter', 'Filter file paths…'); field(fileSection, 'Filter files', filter);
  const fileList = el('div', '', 'download-file-list'); fileList.id = 'download-file-list';
  const selectedSummary = el('p', '', 'small'); selectedSummary.id = 'download-selection';
  const planButton = button('Plan selected files'); planButton.id = 'download-plan-selected';
  fileSection.append(pin, fileList, selectedSummary, planButton); section.append(fileSection);
  const jobHeading = el('div', '', 'surface-title'); jobHeading.append(el('h3', 'Preserved jobs'));
  const refreshButton = button('Refresh', () => refresh()); refreshButton.id = 'download-refresh'; jobHeading.append(refreshButton);
  const jobs = el('div', '', 'download-jobs'); jobs.id = 'download-jobs'; jobs.append(el('p', 'Sign in to inspect preserved downloads.', 'small muted')); section.append(jobHeading, jobs);
  // Keep acquisition discoverable when the authenticated catalog grows. Both
  // sections retain their own DOM roots; a catalog refresh must not replace us.
  panel.insertBefore(section, document.getElementById('models-catalog'));

  function syncAuthentication() { for (const n of section.querySelectorAll('input,select,button')) n.disabled = !isAuthenticated() || state.busy; refreshButton.disabled ||= state.refreshing; }
  function setBusy(value) { state.busy = value; syncAuthentication(); }
  function fail(error) { feedback.textContent = error?.message || String(error); notice(feedback.textContent, 'bad'); }
  function selectedFiles() { return [...fileList.querySelectorAll('input[type="checkbox"]:checked')].map(n => n.value); }
  function selectionSummary() { const names = new Set(selectedFiles()); const size = (state.listing?.files || []).filter(f => names.has(f.path)).reduce((n, f) => n + f.size, 0); selectedSummary.textContent = `${names.size} files · ${bytes(size)} selected. No transfer has started.`; }
  function renderFiles(listing) {
    state.listing = listing; fileSection.hidden = false; filter.value = ''; fileList.replaceChildren();
    pin.textContent = `${listing.source} · ${listing.repo} · revision ${listing.revision}. ${listing.warning || ''}`;
    for (const [index, file] of listing.files.entries()) {
      const row = el('label', '', 'download-file checkbox-label'); const checkbox = el('input'); checkbox.type = 'checkbox'; checkbox.value = file.path; checkbox.id = `download-file-${index}`; row.htmlFor = checkbox.id;
      const text = el('span', `${file.path} · ${bytes(file.size)} · ${file.sha256 ? 'source SHA256 available' : 'no source SHA256'}`);
      row.append(checkbox, text); checkbox.addEventListener('change', selectionSummary); row.dataset.path = file.path.toLowerCase(); fileList.append(row);
    }
    selectionSummary();
  }
  filter.addEventListener('input', () => { const text = filter.value.trim().toLowerCase(); for (const row of fileList.children) row.hidden = !row.dataset.path.includes(text); });
  async function inspect(item) {
    if (!isAuthenticated() || state.busy) return; setBusy(true); feedback.textContent = 'Reading repository metadata; not downloading files…';
    try { const listing = await request(`/v1/downloads/files?source=${encodeURIComponent(item.source)}&repo=${encodeURIComponent(item.repo)}`, { timeout: 30000 }); renderFiles(listing); feedback.textContent = 'Select specific files and review their combined size before creating a plan.'; }
    catch (error) { fail(error); } finally { setBusy(false); }
  }
  searchForm.addEventListener('submit', async event => {
    event.preventDefault(); if (!isAuthenticated() || state.busy) return; setBusy(true); feedback.textContent = 'Searching public repository metadata…';
    try {
      const response = await request(`/v1/downloads/search?source=${encodeURIComponent(source.value)}&q=${encodeURIComponent(query.value.trim())}`, { timeout: 50000 }); results.replaceChildren();
      for (const item of response.items || []) { const row = el('div', '', 'download-result'); const text = el('div'); text.append(el('strong', item.repo), el('p', `${item.source} · ${(item.tags || []).slice(0, 6).join(', ')}`, 'small muted')); row.append(text, button('Inspect files', () => inspect(item))); results.append(row); }
      const warnings = Object.entries(response.warnings || {}).map(([name, warning]) => `${name}: ${warning}`); feedback.textContent = `${response.items?.length || 0} repositories. ${warnings.join(' ')}`;
    } catch (error) { fail(error); } finally { setBusy(false); }
  });
  async function createPlan(body) {
    if (!isAuthenticated() || state.busy) return; setBusy(true);
    try { const job = await request('/v1/downloads/plan', { method: 'POST', body, timeout: 30000 }); feedback.textContent = `Plan ${job.id}: ${job.files.length} files · ${bytes(job.total_bytes)}. Review the job and choose Start to begin.`; await refresh(); }
    catch (error) { fail(error); } finally { setBusy(false); }
  }
  urlForm.addEventListener('submit', event => { event.preventDefault(); const body = { url: direct.value.trim() }; if (hash.value.trim()) body.sha256 = hash.value.trim(); createPlan(body); });
  planButton.addEventListener('click', () => {
    if (!state.listing) return; const files = selectedFiles(); if (!files.length || files.length > 128) { fail(new Error('Select between 1 and 128 files. There is no implicit download-all.')); return; }
    createPlan({ source: state.listing.source, repo: state.listing.repo, revision: state.listing.revision, files });
  });
  async function action(job, name) {
    if (!isAuthenticated()) return;
    const effects = { start: `Download ${bytes(job.total_bytes - job.bytes)} into the managed directory?`, resume: `Resume the preserved partial files (${bytes(job.total_bytes - job.bytes)} remaining)?`, pause: 'Pause acquisition and preserve all partial files?', cancel: 'Cancel acquisition and preserve all partial files? This does not delete files.' };
    if (!window.confirm(`${effects[name]}\n\n${job.destination}\n${job.files.length} files · ${job.source}${job.repo ? ` · ${job.repo}` : ''}\n\nNo model will be loaded or switched.`)) return;
    try { await request(`/v1/downloads/jobs/${encodeURIComponent(job.id)}/${name}`, { method: 'POST', body: { confirm: true }, timeout: 15000 }); await refresh(); }
    catch (error) { fail(new Error(`${error.message} No automatic retry. Refresh the preserved job before resubmitting.`)); }
  }
  function renderJobs() {
    jobs.replaceChildren(); if (!state.jobs.length) { jobs.append(el('p', 'No preserved downloads. Search or paste a public file URL to create a plan.', 'small muted')); return; }
    for (const job of state.jobs) {
      const row = el('article', '', 'download-job'); const heading = el('div', '', 'job-head'); heading.append(el('strong', job.repo || job.files?.[0]?.path || job.id), el('span', job.status, 'badge neutral')); row.append(heading);
      const progress = downloadProgress(job); const meter = el('progress'); meter.max = 1; meter.value = progress.fraction; meter.setAttribute('aria-label', `Downloaded bytes for ${job.id}`); row.append(meter);
      const rate = job.bytes_per_second > 0 ? ` · ${bytes(job.bytes_per_second)}/s` : ''; const eta = Number.isFinite(job.eta_seconds) && job.eta_seconds >= 0 ? ` · ETA ${seconds(job.eta_seconds)}` : '';
      row.append(el('p', `${bytes(progress.bytes)} / ${progress.total === null ? 'unknown' : bytes(progress.total)}${rate}${eta}`, 'small'));
      row.append(el('p', `${job.id} · ${job.destination}`, 'small muted')); if (job.error) row.append(el('p', job.error, 'small')); row.append(el('p', job.warning, 'small muted'));
      const details = el('details'); details.append(el('summary', `${job.files.length} selected files and verification`));
      for (const file of job.files) {
        const fileRow = el('div', '', 'download-job-file');
        fileRow.append(el('p', `${file.path} · ${bytes(file.bytes)} / ${bytes(file.size)} · ${file.status}${file.verification ? ` · ${file.verification}` : ''}`, 'small muted'));
        fileRow.append(el('p', `Commit: ${file.revision || 'not provided'} · SHA256: ${file.sha256 || 'not provided; size verification only'}`, 'small muted'));
        details.append(fileRow);
      }
      row.append(details);
      const controls = el('div', '', 'button-row'); for (const name of downloadJobActions(job.status)) controls.append(button(name[0].toUpperCase() + name.slice(1), () => action(job, name))); row.append(controls); jobs.append(row);
    }
  }
  function schedule() {
    if (state.timer) clearTimeout(state.timer); state.timer = null;
    if (!panel.hidden && !document.hidden && state.connected && state.jobs.some(job => ['RUNNING', 'VERIFYING', 'PAUSING', 'CANCELLING'].includes(job.status))) state.timer = setTimeout(() => refresh(), 2000);
  }
  async function refresh() {
    if (!isAuthenticated()) { disconnect(); return; }
    if (panel.hidden || state.refreshing) return; state.refreshing = true;
    syncAuthentication();
    const epoch = state.authEpoch;
    try {
      const [options, response] = await Promise.all([request('/v1/downloads/options'), request('/v1/downloads/jobs')]); if (epoch !== state.authEpoch) return; state.connected = true; state.jobs = response.jobs || [];
      storage.textContent = `Destination: ${options.destination_root} · ${options.concurrency} active job · ${bytes(options.disk_reserve_bytes)} disk reserve. Public files only; gated/private repositories are not supported.`; renderJobs();
    } catch (error) { if (epoch !== state.authEpoch) return; state.connected = false; storage.textContent = `Download status unavailable: ${error.message}`; }
    finally { state.refreshing = false; syncAuthentication(); schedule(); }
  }
  new MutationObserver(schedule).observe(panel, { attributes: true, attributeFilter: ['hidden'] });
  document.addEventListener('visibilitychange', schedule);
  function disconnect() {
    state.authEpoch++; state.connected = false; state.jobs = []; if (state.timer) clearTimeout(state.timer); state.timer = null;
    state.listing = null; results.replaceChildren(); fileList.replaceChildren(); fileSection.hidden = true; feedback.textContent = '';
    jobs.replaceChildren(el('p', 'Sign in to inspect preserved downloads.', 'small muted')); storage.textContent = 'Sign in to search public sources and inspect the managed destination and preserved download jobs.';
    syncAuthentication();
  }
  syncAuthentication();
  return { refresh, disconnect, syncAuthentication };
}
