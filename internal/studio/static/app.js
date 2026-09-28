const project = document.querySelector("#project");
const serverStatus = document.querySelector("#server-status");
const storageStatus = document.querySelector("#storage-status");
const eventStatus = document.querySelector("#event-status");
const runCount = document.querySelector("#run-count");
const runList = document.querySelector("#runs");
const form = document.querySelector("#manual-run-form");
const formStatus = document.querySelector("#manual-run-status");
const teamSelect = document.querySelector("#team");
const agentSelect = document.querySelector("#agent");
const workspaceSelect = document.querySelector("#workspace");
const harnessSelect = document.querySelector("#harness");
const permissionSelect = document.querySelector("#permission");
const workspacePath = document.querySelector("#workspace-path");
const confirmWorkspace = document.querySelector("#confirm-workspace");
const runDetail = document.querySelector("#run-detail");
const runSummary = document.querySelector("#run-summary");
const runLog = document.querySelector("#run-log");
const changeSummary = document.querySelector("#change-summary");
const cancelRun = document.querySelector("#cancel-run");
const pinRun = document.querySelector("#pin-run");
const deleteRunDetail = document.querySelector("#delete-run-detail");
const runApprovals = document.querySelector("#run-approvals");
const memoryProposals = document.querySelector("#memory-proposals");
const runConclusion = document.querySelector("#run-conclusion");
const outcomeForm = document.querySelector("#outcome-form");
const outcomeStatus = document.querySelector("#outcome-status");
const outcomeList = document.querySelector("#outcomes");
const monitorList = document.querySelector("#monitors");
const workItemTimeline = document.querySelector("#work-item-timeline");
const eventForm = document.querySelector("#event-form");
const eventFormStatus = document.querySelector("#event-form-status");
const routeList = document.querySelector("#routes");
const eventInbox = document.querySelector("#event-inbox");
const eventActions = document.querySelector("#event-actions");
const eventDetail = document.querySelector("#event-detail");
const sourceList = document.querySelector("#sources");
const sourceDetail = document.querySelector("#source-detail");
const definitionForm = document.querySelector("#definition-form");
const definitionSelect = document.querySelector("#definition-file");
const definitionContent = document.querySelector("#definition-content");
const definitionStatus = document.querySelector("#definition-status");
const reloadDefinition = document.querySelector("#reload-definition");

let csrfToken = "";
let catalog = null;
let selectedRunID = "";
let refreshTimer = null;
let selectedDefinition = null;
let availableRoutes = [];

function formatBytes(bytes) {
  if (!Number.isFinite(bytes)) return "unknown";
  const units = ["B", "KiB", "MiB", "GiB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

fetch("/api/v1/health")
  .then((response) => response.json())
  .then((health) => {
    project.textContent = health.project ? `Project: ${health.project}` : "No project selected";
    serverStatus.textContent = "Healthy";
    if (health.storage === "ready") {
      const usage = Number.isFinite(health.storage_bytes)
        ? ` · ${formatBytes(health.storage_bytes)}${Number.isFinite(health.storage_limit_bytes) ? ` / ${formatBytes(health.storage_limit_bytes)}` : ""}`
        : "";
	  const metrics = health.metrics;
	  const diagnosticSummary = metrics ? ` · ${metrics.pending_runs} pending runs · ${metrics.stale_sources} stale sources` : "";
      storageStatus.textContent = `Ready (schema ${health.schema_version})${usage}${diagnosticSummary}`;
    } else {
      storageStatus.textContent = "Unavailable";
    }
  })
  .catch(() => {
    project.textContent = "Studio health check failed";
    serverStatus.textContent = "Unavailable";
    storageStatus.textContent = "Unavailable";
  });

function refreshRuns() {
  return fetch("/api/v1/runs")
  .then((response) => {
    if (!response.ok) throw new Error("runtime storage unavailable");
    return response.json();
  })
  .then((result) => {
    runCount.textContent = String(result.runs.length);
    runList.replaceChildren();
    if (result.runs.length === 0) {
      const empty = document.createElement("li");
      empty.textContent = "No runs yet.";
      runList.append(empty);
      return;
    }
    for (const run of result.runs) {
      const item = document.createElement("li");
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = `${run.state} · ${run.team}/${run.agent} · ${run.harness} · ${run.permission}`;
      button.addEventListener("click", () => {
        selectedRunID = run.id;
        refreshRunDetail();
      });
      item.append(button);
      runList.append(item);
    }
	if (selectedRunID) refreshRunDetail();
  })
  .catch(() => {
    runCount.textContent = "Unavailable";
    runList.replaceChildren();
  });
}

function scheduleRunRefresh() {
  if (refreshTimer !== null) return;
  refreshTimer = setTimeout(() => {
    refreshTimer = null;
    refreshRuns();
  }, 50);
}

function decodeLog(text) {
  return text.trim().split("\n").filter(Boolean).map((line) => {
    try {
      const record = JSON.parse(line);
      const binary = atob(record.data_base64);
      const bytes = Uint8Array.from(binary, (value) => value.charCodeAt(0));
      return `[${record.stream}] ${new TextDecoder().decode(bytes)}`;
    } catch {
      return "[invalid log record]";
    }
  }).join("");
}

async function refreshRunDetail() {
  if (!selectedRunID) return;
  const response = await fetch(`/api/v1/runs/${encodeURIComponent(selectedRunID)}`);
  if (!response.ok) return;
  const detail = await response.json();
  runDetail.hidden = false;
  runSummary.textContent = `${detail.run.state} · ${detail.run.conclusion || "in progress"}`;
  pinRun.textContent = detail.run.pinned ? "Unpin run" : "Pin run";
  pinRun.dataset.pinned = String(Boolean(detail.run.pinned));
  deleteRunDetail.hidden = !["succeeded", "failed", "cancelled", "interrupted"].includes(detail.run.state) || detail.run.pinned;
  cancelRun.hidden = !["pending", "leased", "preparing", "running", "waiting-for-approval"].includes(detail.run.state);
  runApprovals.replaceChildren();
  for (const approval of detail.approvals ?? []) {
    const row = document.createElement("div");
    const description = document.createElement("p");
    description.textContent = `${approval.state} · ${approval.summary || approval.kind} · ${JSON.stringify(approval.scope)}`;
    row.append(description);
    if (approval.state === "pending") {
      for (const decision of ["approved", "denied"]) {
        const button = document.createElement("button");
        button.type = "button";
        button.textContent = decision === "approved" ? "Approve for this run" : "Deny";
        button.addEventListener("click", () => decideApproval(approval.id, decision));
        row.append(button);
      }
    }
    runApprovals.append(row);
  }
  runConclusion.textContent = detail.structured_conclusion
    ? JSON.stringify(detail.structured_conclusion.conclusion, null, 2)
    : "No structured conclusion is available yet.";
  memoryProposals.replaceChildren();
  for (const proposal of detail.memory_proposals ?? []) {
    const wrapper = document.createElement("div");
    const heading = document.createElement("p");
    heading.textContent = `${proposal.state} memory proposal · ${proposal.target_path}`;
    const diff = document.createElement("pre");
    diff.textContent = proposal.diff;
    wrapper.append(heading, diff);
    if (proposal.state === "pending") {
      for (const decision of ["approved", "rejected"]) {
        const button = document.createElement("button");
        button.type = "button";
        button.textContent = decision === "approved" ? "Approve memory edit" : "Reject memory edit";
        button.addEventListener("click", () => decideMemoryProposal(proposal.id, decision));
        wrapper.append(button);
      }
    }
    memoryProposals.append(wrapper);
  }
  const attempt = detail.attempts.at(-1);
  const changes = attempt?.result?.changes;
  if (changes) {
    const parts = [];
    if (changes.added?.length) parts.push(`Added: ${changes.added.join(", ")}`);
    if (changes.modified?.length) parts.push(`Modified: ${changes.modified.join(", ")}`);
    if (changes.deleted?.length) parts.push(`Deleted: ${changes.deleted.join(", ")}`);
    if (changes.incomplete) parts.push("Summary is incomplete because the bounded scan could not inspect everything.");
    changeSummary.textContent = parts.join(" · ") || "No workspace changes recorded.";
  } else {
    changeSummary.textContent = "Workspace change summary pending.";
  }
  if (!attempt?.log_url) {
    runLog.textContent = attempt ? "Waiting for output…" : "Waiting for an attempt…";
    return;
  }
  const logResponse = await fetch(attempt.log_url);
  if (logResponse.ok) runLog.textContent = decodeLog(await logResponse.text()) || "No output.";
}

async function decideMemoryProposal(id, decision) {
  const response = await fetch(`/api/v1/memory-proposals/${encodeURIComponent(id)}/decision`, {
    method: "POST", headers: {"Content-Type":"application/json", "X-AgentWorks-CSRF": csrfToken},
    body: JSON.stringify({decision}),
  });
  const result = await response.json();
  if (!response.ok) formStatus.textContent = result.error ?? "Could not decide memory proposal";
  await refreshRunDetail();
}

async function refreshOutcomesAndMonitors() {
  const [outcomeResponse, monitorResponse] = await Promise.all([fetch("/api/v1/outcomes"), fetch("/api/v1/monitors")]);
  if (outcomeResponse.ok) {
    const result = await outcomeResponse.json(); outcomeList.replaceChildren();
    for (const outcome of result.outcomes) {
      const item=document.createElement("li"); const button=document.createElement("button"); button.type="button";
      button.textContent=`${outcome.type} · ${outcome.subject || outcome.work_item_id || "uncorrelated"}`; button.disabled=!outcome.work_item_id;
      button.addEventListener("click",async()=>{const response=await fetch(`/api/v1/work-items/${encodeURIComponent(outcome.work_item_id)}/timeline`);if(response.ok){const timeline=await response.json();workItemTimeline.textContent=JSON.stringify(timeline.timeline,null,2);}});
      item.append(button); outcomeList.append(item);
    }
    if (!result.outcomes.length) outcomeList.innerHTML="<li>No outcomes yet.</li>";
  }
  if (monitorResponse.ok) {
    const result=await monitorResponse.json(); monitorList.replaceChildren();
    for (const monitor of result.monitors) { const item=document.createElement("li"); item.textContent=`${monitor.state} · ${monitor.monitor_id}`; monitorList.append(item); }
    if (!result.monitors.length) monitorList.innerHTML="<li>No monitor evaluations yet.</li>";
  }
}

outcomeForm.addEventListener("submit", async (event) => {
  event.preventDefault(); outcomeStatus.textContent="Recording…";
  const kind=document.querySelector("#work-item-kind").value.trim(); const key=document.querySelector("#work-item-key").value.trim();
  const payload={type:document.querySelector("#outcome-type").value.trim(),subject:document.querySelector("#outcome-subject").value.trim(),data:{}};
  if (kind && key) payload.work_item={kind,external_key:key,data:{}};
  const response=await fetch("/api/v1/outcomes",{method:"POST",headers:{"Content-Type":"application/json","X-AgentWorks-CSRF":csrfToken},body:JSON.stringify(payload)});
  const result=await response.json(); outcomeStatus.textContent=response.ok?"Outcome recorded.":(result.error??"Could not record outcome"); if(response.ok){outcomeForm.reset();await refreshOutcomesAndMonitors();}
});

function eventPayload() {
  return {specversion:"1.0",id:document.querySelector("#event-id").value.trim(),source:document.querySelector("#event-source").value.trim(),type:document.querySelector("#event-type").value.trim(),subject:document.querySelector("#event-subject").value.trim(),time:new Date().toISOString(),data:JSON.parse(document.querySelector("#event-data").value)};
}

async function eventCommand(url,payload={}) {
  return fetch(url,{method:"POST",headers:{"Content-Type":"application/json","X-AgentWorks-CSRF":csrfToken},body:JSON.stringify(payload)});
}

async function refreshRoutesAndInbox() {
  const [routesResponse,inboxResponse]=await Promise.all([fetch("/api/v1/routes"),fetch("/api/v1/inbox")]);
  if(routesResponse.ok){const result=await routesResponse.json();availableRoutes=result.routes??[];routeList.replaceChildren();const states=new Map(result.subscriptions.map(item=>[item.id,item]));for(const route of availableRoutes){const item=document.createElement("li");const state=states.get(route.name);const button=document.createElement("button");button.type="button";button.textContent=`${state?.enabled?"enabled":"disabled"} · ${route.priority} · ${route.name}`;button.addEventListener("click",async()=>{await eventCommand(`/api/v1/routes/${encodeURIComponent(route.name)}/enabled`,{enabled:!state?.enabled});await refreshRoutesAndInbox();});item.append(button);routeList.append(item)}for(const fixture of result.fixtures??[]){const item=document.createElement("li");item.textContent=`${fixture.passed?"fixture passed":"fixture failed"} · ${fixture.route}/${fixture.name} · expected ${fixture.expect}, got ${fixture.actual}`;routeList.append(item)}for(const issue of result.issues){const item=document.createElement("li");item.textContent=`invalid · ${issue.path} · ${issue.error}`;routeList.append(item)}if(!availableRoutes.length&&!result.issues.length)routeList.innerHTML="<li>No routes loaded.</li>";}
  if(inboxResponse.ok){const result=await inboxResponse.json();eventInbox.replaceChildren();for(const event of result.events){const item=document.createElement("li");const button=document.createElement("button");button.type="button";button.textContent=`${event.status} · ${event.source} · ${event.type} · ${event.subject||event.external_id}`;button.addEventListener("click",()=>inspectEvent(event.record_id));item.append(button);eventInbox.append(item)}if(!result.events.length)eventInbox.innerHTML="<li>No events yet.</li>";}
}

async function refreshSources(){const response=await fetch("/api/v1/sources");if(!response.ok)return;const result=await response.json();sourceList.replaceChildren();const definitions=new Map((result.definitions??[]).map(item=>[item.name,item]));for(const source of result.sources??[]){const item=document.createElement("li");const summary=document.createElement("span");summary.textContent=`${source.paused?"paused":source.state} · ${source.kind} · ${source.id} · ${source.last_success_at||"never polled"}${source.failure_count?` · failures: ${source.failure_count}`:""}${source.last_error?` · ${source.last_error}`:""}`;item.append(summary);for(const action of ["test","poll"]){const button=document.createElement("button");button.type="button";button.textContent=action==="test"?"Test connection":"Poll now";button.addEventListener("click",async()=>{const response=await eventCommand(`/api/v1/sources/${encodeURIComponent(source.id)}/${action}`);const body=await response.json();sourceDetail.textContent=JSON.stringify(body,null,2);await Promise.all([refreshSources(),refreshRoutesAndInbox(),refreshRuns(),refreshOutcomesAndMonitors()])});item.append(button)}const history=document.createElement("button");history.type="button";history.textContent="Diagnostics";history.addEventListener("click",async()=>{const response=await fetch(`/api/v1/sources/${encodeURIComponent(source.id)}/attempts`);sourceDetail.textContent=JSON.stringify(await response.json(),null,2)});item.append(history);const pause=document.createElement("button");pause.type="button";pause.textContent=source.paused?"Resume":"Pause";pause.addEventListener("click",async()=>{const response=await eventCommand(`/api/v1/sources/${encodeURIComponent(source.id)}/pause`,{paused:!source.paused});sourceDetail.textContent=JSON.stringify(await response.json(),null,2);await refreshSources()});item.append(pause);sourceList.append(item);definitions.delete(source.id)}for(const definition of definitions.values()){if(definition.kind!=="jira"&&definition.kind!=="github")continue;const item=document.createElement("li");item.textContent=`not synchronized · ${definition.kind} · ${definition.name}`;sourceList.append(item)}for(const issue of result.issues??[]){const item=document.createElement("li");item.textContent=`invalid · ${issue.path} · ${issue.error}`;sourceList.append(item)}if(!sourceList.children.length)sourceList.innerHTML="<li>No external sources loaded.</li>"}

async function inspectEvent(id){const response=await fetch(`/api/v1/inbox/${encodeURIComponent(id)}`);if(!response.ok)return;const result=await response.json();eventActions.replaceChildren();eventDetail.textContent=JSON.stringify(result,null,2);if(result.event.status==="unrouted"){const matched=new Set(result.decision.matches.map(item=>item.route));for(const route of availableRoutes){const button=document.createElement("button");button.type="button";button.textContent=`Route now via ${route.name}${matched.has(route.name)?" (matched)":""}`;button.addEventListener("click",async()=>{await eventCommand(`/api/v1/inbox/${encodeURIComponent(id)}/route`,{route:route.name});await refreshRoutesAndInbox();await inspectEvent(id)});eventActions.append(button)}const reevaluate=document.createElement("button");reevaluate.type="button";reevaluate.textContent="Re-evaluate";reevaluate.addEventListener("click",async()=>{await eventCommand(`/api/v1/inbox/${encodeURIComponent(id)}/reevaluate`);await refreshRoutesAndInbox();await inspectEvent(id)});eventActions.append(reevaluate)}const replay=document.createElement("button");replay.type="button";replay.textContent="Replay as a fresh event";replay.addEventListener("click",async()=>{const replayID=window.prompt("Unique replay event ID",`${result.event.external_id}:replay:${Date.now()}`);if(!replayID)return;await eventCommand(`/api/v1/inbox/${encodeURIComponent(id)}/replay`,{replay_id:replayID});await refreshRoutesAndInbox()});eventActions.append(replay)}

document.querySelector("#preview-event").addEventListener("click",async()=>{try{const response=await eventCommand("/api/v1/routes/preview",eventPayload());const result=await response.json();eventFormStatus.textContent=response.ok?`${result.decision.reason}${result.decision.selected?` → ${result.decision.selected}`:""}`:(result.error??"Preview failed")}catch(error){eventFormStatus.textContent=error.message}});
eventForm.addEventListener("submit",async(event)=>{event.preventDefault();try{const response=await eventCommand("/api/v1/event-ingest",eventPayload());const result=await response.json();eventFormStatus.textContent=response.ok?`${result.event.status} · ${result.decision.reason}`:(result.error??"Ingestion failed");if(response.ok){await refreshRoutesAndInbox()}}catch(error){eventFormStatus.textContent=error.message}});

async function decideApproval(approvalID, decision) {
  const response = await fetch(`/api/v1/approvals/${encodeURIComponent(approvalID)}/decision`, {
    method: "POST",
    headers: {"Content-Type": "application/json", "X-AgentWorks-CSRF": csrfToken},
    body: JSON.stringify({decision}),
  });
  const result = await response.json();
  if (!response.ok) formStatus.textContent = result.error ?? "Could not decide approval";
  await refreshRunDetail();
}

cancelRun.addEventListener("click", async () => {
  if (!selectedRunID) return;
  const response = await fetch(`/api/v1/runs/${encodeURIComponent(selectedRunID)}/cancel`, {
    method: "POST",
    headers: {"X-AgentWorks-CSRF": csrfToken},
  });
  if (!response.ok) {
    const result = await response.json();
    formStatus.textContent = result.error ?? "Could not cancel run";
  }
  await refreshRuns();
});

pinRun.addEventListener("click", async () => {
  if (!selectedRunID) return;
  const pinned = pinRun.dataset.pinned !== "true";
  const response = await fetch(`/api/v1/runs/${encodeURIComponent(selectedRunID)}/pin`, {
    method: "POST",
    headers: {"Content-Type": "application/json", "X-AgentWorks-CSRF": csrfToken},
    body: JSON.stringify({pinned}),
  });
  if (!response.ok) formStatus.textContent = (await response.json()).error ?? "Could not update pin";
  await Promise.all([refreshRuns(), refreshRunDetail()]);
});

deleteRunDetail.addEventListener("click", async () => {
  if (!selectedRunID || !window.confirm("Delete raw logs and event payload for this run? Summaries, outcomes, approvals, monitors, and audit records remain.")) return;
  const response = await fetch(`/api/v1/runs/${encodeURIComponent(selectedRunID)}/detail`, {
    method: "DELETE",
    headers: {"X-AgentWorks-CSRF": csrfToken},
  });
  if (!response.ok) formStatus.textContent = (await response.json()).error ?? "Could not delete run detail";
  await refreshRunDetail();
});

function appendOptions(select, values, label) {
  select.replaceChildren();
  for (const value of values) {
    const option = document.createElement("option");
    option.value = value.value;
    option.textContent = value.label ?? value.value;
    option.disabled = value.disabled ?? false;
    select.append(option);
  }
  if (values.length === 0) {
    const option = document.createElement("option");
    option.textContent = label;
    option.disabled = true;
    select.append(option);
  }
}

function updateAgents() {
  const team = catalog?.teams.find((item) => item.name === teamSelect.value);
  appendOptions(agentSelect, (team?.agents ?? []).map((agent) => ({
    value: agent.name,
    label: `${agent.name} (max: ${agent.max_permission})`,
  })), "No agents");
  if (team?.default_agent) agentSelect.value = team.default_agent;
}

function renderCatalog(loadedCatalog) {
  catalog = loadedCatalog;
  appendOptions(teamSelect, catalog.teams.map((team) => ({ value: team.name })), "No teams");
  appendOptions(workspaceSelect, catalog.workspaces.map((workspace) => ({
    value: workspace.name,
    label: workspace.bound ? workspace.name : `${workspace.name} (not bound)`,
    disabled: !workspace.bound,
  })), "No workspaces");
  appendOptions(harnessSelect, catalog.harnesses.map((value) => ({ value })), "No harnesses");
  appendOptions(permissionSelect, catalog.permissions.map((value) => ({ value })), "No permissions");
  updateAgents();
}

function definitionURL(path) {
  return `/api/v1/definitions/${path.split("/").map(encodeURIComponent).join("/")}`;
}

async function loadDefinition() {
  const path = definitionSelect.value;
  if (!path) return;
  definitionStatus.textContent = "Loading…";
  const response = await fetch(definitionURL(path));
  const result = await response.json();
  if (!response.ok) {
    definitionStatus.textContent = result.error ?? "Could not load definition";
    return;
  }
  selectedDefinition = result;
  definitionContent.value = result.content;
  definitionStatus.textContent = `${result.kind} · ${result.size} bytes`;
}

async function refreshDefinitions() {
  const response = await fetch("/api/v1/definitions");
  const result = await response.json();
  if (!response.ok) throw new Error(result.error ?? "Definitions unavailable");
  const previous = definitionSelect.value;
  appendOptions(definitionSelect, result.files.map((file) => ({
    value: file.path,
    label: `${file.kind} · ${file.path}`,
  })), "No editable definitions");
  if (result.files.some((file) => file.path === previous)) definitionSelect.value = previous;
  await loadDefinition();
}

async function checkDefinitionChanges() {
  if (!selectedDefinition) return;
  try {
    const response = await fetch("/api/v1/definitions");
    if (!response.ok) return;
    const result = await response.json();
    const current = result.files.find((file) => file.path === selectedDefinition.path);
    if (!current || current.sha256 === selectedDefinition.sha256) return;
    if (definitionContent.value === selectedDefinition.content) {
      await loadDefinition();
      definitionStatus.textContent = "Reloaded an external file change.";
    } else {
      definitionStatus.textContent = "This file changed on disk while you have unsaved edits. Reload or copy your edits before saving.";
    }
  } catch {
    // The next poll or explicit reload will retry; runtime health remains separate.
  }
}

Promise.all([
  fetch("/api/v1/session").then((response) => response.json()),
  fetch("/api/v1/catalog").then((response) => {
    if (!response.ok) throw new Error("catalog unavailable");
    return response.json();
  }),
]).then(([session, loadedCatalog]) => {
  csrfToken = session.csrf_token;
  renderCatalog(loadedCatalog);
  return refreshDefinitions();
}).catch((error) => {
  formStatus.textContent = error.message;
  form.querySelector("button").disabled = true;
});

teamSelect.addEventListener("change", updateAgents);
definitionSelect.addEventListener("change", loadDefinition);
reloadDefinition.addEventListener("click", loadDefinition);

definitionForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!selectedDefinition || selectedDefinition.path !== definitionSelect.value) return;
  definitionStatus.textContent = "Saving…";
  try {
    const response = await fetch(definitionURL(selectedDefinition.path), {
      method: "PUT",
      headers: {"Content-Type": "application/json", "X-AgentWorks-CSRF": csrfToken},
      body: JSON.stringify({expected_sha256: selectedDefinition.sha256, content: definitionContent.value}),
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error ?? "Could not save definition");
    selectedDefinition = result;
    definitionStatus.textContent = "Saved to the project folder.";
    const catalogResponse = await fetch("/api/v1/catalog");
    if (catalogResponse.ok) renderCatalog(await catalogResponse.json());
    await refreshDefinitions();
    await refreshSources();
  } catch (error) {
    definitionStatus.textContent = error.message;
  }
});

confirmWorkspace.addEventListener("click", async () => {
  const path = workspacePath.value.trim();
  if (!path) {
    formStatus.textContent = "Enter an absolute folder path to confirm.";
    return;
  }
  formStatus.textContent = "Confirming folder…";
  try {
    const response = await fetch("/api/v1/workspaces/confirm", {
      method: "POST",
      headers: {"Content-Type": "application/json", "X-AgentWorks-CSRF": csrfToken},
      body: JSON.stringify({path}),
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error ?? "Could not confirm folder");
    let option = Array.from(workspaceSelect.options).find((item) => item.value === result.alias);
    if (!option) {
      option = document.createElement("option");
      option.value = result.alias;
      workspaceSelect.append(option);
    }
    option.textContent = `Ad hoc: ${result.path}`;
    workspaceSelect.value = result.alias;
    formStatus.textContent = "Folder confirmed for this Studio session.";
  } catch (error) {
    formStatus.textContent = error.message;
  }
});

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  formStatus.textContent = "Queuing…";
  const payload = {
    request_id: crypto.randomUUID(),
    team: teamSelect.value,
    agent: agentSelect.value,
    workspace: workspaceSelect.value,
    harness: harnessSelect.value,
    permission: permissionSelect.value,
    prompt: document.querySelector("#prompt").value,
  };
  try {
    const response = await fetch("/api/v1/runs", {
      method: "POST",
      headers: {"Content-Type": "application/json", "X-AgentWorks-CSRF": csrfToken},
      body: JSON.stringify(payload),
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error ?? "Could not queue run");
    formStatus.textContent = `Queued ${result.run.id} with ${result.run.permission} permission.`;
    document.querySelector("#prompt").value = "";
    await refreshRuns();
  } catch (error) {
    formStatus.textContent = error.message;
  }
});

refreshRuns();
refreshOutcomesAndMonitors();
refreshRoutesAndInbox();
refreshSources();
// A slow poll is only a safety net; durable SSE events drive normal refreshes.
setInterval(refreshRuns, 15000);
setInterval(checkDefinitionChanges, 2000);

const events = new EventSource("/api/v1/events");
events.addEventListener("studio.ready", () => {
  eventStatus.textContent = "Live";
});
events.addEventListener("runtime.event", () => {
  eventStatus.textContent = "Live";
  scheduleRunRefresh();
  refreshOutcomesAndMonitors();
  refreshRoutesAndInbox();
  refreshSources();
});
events.onerror = () => {
  eventStatus.textContent = "Reconnecting";
};
