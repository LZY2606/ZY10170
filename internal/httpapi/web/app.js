"use strict";

const state = {
  data: null,
  uttId: "u01",
  gen: 1,
  tool: "drag",
  // 路径重选: 起止帧
  path: null,
};

const RANK_COLORS = { 1: "#ff8f6b", 2: "#ffd166", 3: "#7bd88f", 4: "#6bb6ff" };
const RANK_NAMES = { 1: "F1", 2: "F2", 3: "F3", 4: "F4" };
const MARGIN = { left: 56, right: 16, top: 30, bottom: 34 };
const FMAX = 3500;

const $ = (id) => document.getElementById(id);

async function api(path, opts) {
  const res = await fetch(path, opts || {});
  if (!res.ok) {
    const t = await res.text();
    throw new Error(t);
  }
  if (res.status === 204) return null;
  return res.json();
}

function curView() { return state.data.view[state.uttId]; }
function curUtt() { return curView().utterance; }
function curGenView() {
  return curView().perGen[state.gen === 1 ? "gen1" : "gen2"];
}
function frameCount() { return curUtt().numFrames; }

function geom() {
  const svg = $("chart");
  const W = Math.max(760, svg.clientWidth || 900);
  const H = 540;
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
  const innerW = W - MARGIN.left - MARGIN.right;
  const innerH = H - MARGIN.top - MARGIN.bottom;
  const n = frameCount();
  const x = (f) => MARGIN.left + (f + 0.5) * (innerW / n);
  const y = (hz) => MARGIN.top + innerH - (hz / FMAX) * innerH;
  return { W, H, innerW, innerH, x, y };
}

function el(tag, attrs) {
  const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const k in attrs) node.setAttribute(k, attrs[k]);
  return node;
}

// frameFromEvent 根据鼠标 x 反推帧号(最近帧)。
function frameFromEvent(evt) {
  const svg = $("chart");
  const rect = svg.getBoundingClientRect();
  const g = geom();
  const px = (evt.clientX - rect.left) * (g.W / rect.width);
  const n = frameCount();
  const f = Math.floor((px - MARGIN.left) / (g.innerW / n));
  return Math.max(0, Math.min(n - 1, f));
}

// candidatesAt 返回某代某帧候选(缺帧为 [])。
function candidatesAt(gen, frame) {
  const v = curView();
  const gc = v.gens.find((x) => x.gen === gen && x.frame === frame);
  return gc ? gc.candidates : [];
}
function autoAt(gen, frame, rank) {
  return curView().autos.find((a) => a.gen === gen && a.frame === frame && a.rank === rank);
}
function vowelAt(frame) {
  return curView().vowels.find((vv) => frame >= vv.start && frame < vv.end);
}
function conflictsForFrame(frame) {
  return curView().conflicts.filter((c) => c.frame === frame && !c.resolved);
}

function render() {
  const svg = $("chart");
  svg.innerHTML = "";
  const g = geom();
  const { x, y } = g;

  // 背景
  svg.appendChild(el("rect", {
    x: MARGIN.left, y: MARGIN.top, width: g.innerW, height: g.innerH,
    fill: "#0c1120",
  }));

  // 声谱网格 + 频段刻度
  for (let hz = 0; hz <= FMAX; hz += 500) {
    svg.appendChild(el("line", {
      x1: MARGIN.left, x2: MARGIN.left + g.innerW,
      y1: y(hz), y2: y(hz), stroke: "#202a42", "stroke-width": 1,
    }));
    const t = el("text", { x: 8, y: y(hz) + 4, fill: "#93a0b8", "font-size": 11 });
    t.textContent = hz;
    svg.appendChild(t);
  }

  // 声谱热力: 以候选能量绘制半透明条带, 弱能量区可见变暗。
  drawSpectrogram(svg, g);

  // 元音区间(半开 [start,end))
  for (const vv of curView().vowels) {
    const x0 = x(vv.start);
    const x1 = x(vv.end);
    svg.appendChild(el("rect", {
      x: x0, y: MARGIN.top, width: x1 - x0, height: g.innerH,
      fill: "#5ad1ff", opacity: 0.07,
    }));
    svg.appendChild(el("line", {
      x1: x0, x2: x0, y1: MARGIN.top, y2: MARGIN.top + g.innerH,
      stroke: "#5ad1ff", "stroke-width": 1.5, "stroke-dasharray": "4 3",
    }));
    // 右端点(end)不属于当前元音: 用空心竖线标示边界外侧。
    svg.appendChild(el("line", {
      x1: x1, x2: x1, y1: MARGIN.top, y2: MARGIN.top + g.innerH,
      stroke: "#93a0b8", "stroke-width": 1, "stroke-dasharray": "2 4",
    }));
    const t = el("text", { x: x0 + 6, y: MARGIN.top + 14, fill: "#bfe8ff", "font-size": 11 });
    t.textContent = vv.label + " [" + vv.start + "," + vv.end + ")";
    svg.appendChild(t);
  }

  // 缺帧阴影(不补零, 轨迹在缺口处断开)
  const n = frameCount();
  for (let f = 0; f < n; f++) {
    if (candidatesAt(state.gen, f).length === 0) {
      svg.appendChild(el("rect", {
        x: x(f) - (g.innerW / n) / 2, y: MARGIN.top,
        width: g.innerW / n, height: g.innerH, fill: "#000", opacity: 0.45,
      }));
    }
  }

  drawAllCandidates(svg, g);
  drawTracks(svg, g);
  drawPathSelection(svg, g);
  drawFrameAxis(svg, g);
  drawLegend(svg, g);
}

function drawSpectrogram(svg, g) {
  const { x, y } = g;
  const n = frameCount();
  const bw = g.innerW / n;
  // 频段能量带: 按每帧候选能量绘竖带; 无候选区域留黑。
  for (let f = 0; f < n; f++) {
    const cs = candidatesAt(state.gen, f);
    if (cs.length === 0) continue;
    for (const c of cs) {
      const band = el("ellipse", {
        cx: x(f), cy: y(c.frequency),
        rx: bw * 0.62, ry: 26,
        fill: "#ff8f3c", opacity: Math.min(0.28, 0.06 + c.energy * 0.22),
      });
      svg.appendChild(band);
    }
  }
}

function drawAllCandidates(svg, g) {
  const { x, y } = g;
  const n = frameCount();
  for (let f = 0; f < n; f++) {
    const cs = candidatesAt(state.gen, f);
    cs.forEach((c) => {
      const d = el("circle", {
        cx: x(f), cy: y(c.frequency), r: 2.6,
        fill: "#aebbd4", opacity: 0.55,
      });
      svg.appendChild(d);
    });
  }
}

// drawTracks 绘制 F1..F4 最终轨迹: 缺帧处断线, 不跨缺口连接。
function drawTracks(svg, g) {
  const { x, y } = g;
  const rows = curGenView().final;
  for (let rank = 1; rank <= 4; rank++) {
    let seg = [];
    const flush = () => {
      if (seg.length < 2) { seg = []; return; }
      const pts = seg.map((p) => `${x(p.frame)},${y(p.frequency)}`).join(" ");
      svg.appendChild(el("polyline", {
        points: pts, fill: "none",
        stroke: RANK_COLORS[rank], "stroke-width": 2, opacity: 0.9,
      }));
      seg = [];
    };
    rows.forEach((row, f) => {
      const p = row.find((q) => q.rank === rank);
      if (!p) { flush(); return; }
      seg.push({ frame: f, frequency: p.frequency });
      if (seg.length === 1) {
        // 段起点; 等待下一点决定是否成线
      }
    });
    flush();

    // 点
    rows.forEach((row, f) => {
      const p = row.find((q) => q.rank === rank);
      if (!p) return;
      const circle = el("circle", {
        cx: x(f), cy: y(p.frequency), r: p.edited ? 5.5 : 4,
        fill: RANK_COLORS[rank],
        stroke: p.edited ? "#ffffff" : "#0f1420",
        "stroke-width": p.edited ? 1.6 : 1,
        class: "track-point",
        "data-frame": f, "data-rank": rank,
      });
      circle.style.cursor = "pointer";
      svg.appendChild(circle);
    });
  }

  // 冲突帧高亮
  const confs = curView().conflicts || [];
  confs.forEach((c) => {
    if (c.resolved) return;
    svg.appendChild(el("circle", {
      cx: x(c.frame), cy: y(c.oldFrequency), r: 9,
      fill: "none", stroke: "#ffb454", "stroke-width": 2,
      "stroke-dasharray": "3 2",
    }));
  });
}

function drawPathSelection(svg, g) {
  if (!state.path) return;
  const { x } = g;
  const a = Math.min(state.path.start, state.path.end);
  const b = Math.max(state.path.start, state.path.end);
  svg.appendChild(el("rect", {
    x: x(a) - g.innerW / frameCount() / 2, y: MARGIN.top,
    width: (b - a + 1) * (g.innerW / frameCount()),
    height: g.innerH, fill: "#5ad1ff", opacity: 0.08,
  }));
}

function drawFrameAxis(svg, g) {
  const { x, y } = g;
  const n = frameCount();
  for (let f = 0; f < n; f += 4) {
    svg.appendChild(el("line", {
      x1: x(f), x2: x(f), y1: MARGIN.top + g.innerH,
      y2: MARGIN.top + g.innerH + 5, stroke: "#3a4straw".replace("straw", "666"),
    }));
    const t = el("text", {
      x: x(f) - 10, y: MARGIN.top + g.innerH + 22,
      fill: "#93a0b8", "font-size": 11,
    });
    t.textContent = f * curUtt().frameMs + "ms";
    svg.appendChild(t);
  }
  const base = el("line", {
    x1: MARGIN.left, x2: MARGIN.left + g.innerW,
    y1: y(0), y2: y(0), stroke: "#3a4666",
  });
  svg.appendChild(base);
}

function drawLegend(svg, g) {
  const names = ["F1", "F2", "F3", "F4"];
  names.forEach((nm, i) => {
    const rank = i + 1;
    const lx = MARGIN.left + 12 + i * 70;
    const ly = MARGIN.top - 12;
    svg.appendChild(el("line", {
      x1: lx, x2: lx + 22, y1: ly, y2: ly,
      stroke: RANK_COLORS[rank], "stroke-width": 3,
    }));
    const t = el("text", { x: lx + 26, y: ly + 4, fill: "#cdd7ea", "font-size": 11 });
    t.textContent = nm;
    svg.appendChild(t);
  });
  const t2 = el("text", {
    x: g.W - 250, y: MARGIN.top - 12, fill: "#93a0b8", "font-size": 11,
  });
  t2.textContent = "灰点=原算法每个候选; 彩色=F1–F4 最终轨迹; 橙圈=歧义";
  svg.appendChild(t2);
}

// ---- 交互 ----
let dragCtx = null;

function nearestCandidate(frame, hz, exclude) {
  const cs = candidatesAt(state.gen, frame).filter((c) => !exclude || c.id !== exclude);
  let best = null;
  cs.forEach((c) => {
    if (!best || Math.abs(c.frequency - hz) < Math.abs(best.frequency - hz)) best = c;
  });
  return best;
}

function setupPointer() {
  const svg = $("chart");

  svg.addEventListener("mousedown", (evt) => {
    const node = evt.target.closest ? evt.target.closest(".track-point") : null;
    const f = frameFromEvent(evt);
    if (state.tool === "drag" && node) {
      const frame = +node.dataset.frame;
      const rank = +node.dataset.rank;
      const row = curGenView().final[frame];
      const p = row.find((q) => q.rank === rank);
      dragCtx = { frame, rank, fromId: p.candidateId };
      evt.preventDefault();
    } else if (state.tool === "path") {
      if (state.path == null) {
        state.path = { start: f, end: f };
        state.pathPicked = {};
      } else {
        const a = Math.min(state.path.start, state.path.end);
        const b = Math.max(state.path.start, state.path.end);
        // 在已有区间内按下视为选择候选, 不重设区间; 区间外才开始新区间。
        if (f < a || f > b) {
          state.path = { start: f, end: f };
          state.pathPicked = {};
        }
      }
      render();
    } else if (state.tool === "lock") {
      tryLockAt(f);
    }
  });

  svg.addEventListener("mousemove", (evt) => {
    if (!dragCtx) return;
    const g = geom();
    const rect = svg.getBoundingClientRect();
    const py = (evt.clientY - rect.top) * (g.H / rect.height);
    const innerH = g.innerH;
    const hz = Math.max(0, Math.min(FMAX,
      (FMAX) * (MARGIN.top + innerH - py) / innerH));
    const cand = nearestCandidate(dragCtx.frame, hz, dragCtx.fromId);
    if (cand) $("hint").textContent =
      `拖到候选 ${cand.frequency.toFixed(0)}Hz (帧 ${dragCtx.frame}, ${RANK_NAMES[dragCtx.rank]})`;
  });

  window.addEventListener("mouseup", async (evt) => {
    if (!dragCtx) return;
    const g = geom();
    const rect = svg.getBoundingClientRect();
    const py = (evt.clientY - rect.top) * (g.H / rect.height);
    const innerH = g.innerH;
    const hz = FMAX * (MARGIN.top + innerH - py) / innerH;
    const cand = nearestCandidate(dragCtx.frame, hz, dragCtx.fromId);
    const ctx = dragCtx;
    dragCtx = null;
    if (!cand || cand.id === ctx.fromId) { render(); return; }
    await submitOp({
      utteranceId: state.uttId, gen: state.gen, type: "drag",
      frame: ctx.frame, rank: ctx.rank,
      drag: { fromCandidateId: ctx.fromId, toCandidateId: cand.id },
      note: `拖点 f${ctx.frame} ${RANK_NAMES[ctx.rank]} -> ${cand.frequency.toFixed(0)}Hz`,
    });
  });

  // 路径模式下点击帧内候选, 累积重选
  svg.addEventListener("click", (evt) => {
    if (state.tool !== "path" || !state.path) return;
    const f = frameFromEvent(evt);
    const a = Math.min(state.path.start, state.path.end);
    const b = Math.max(state.path.start, state.path.end);
    if (f < a || f > b) return;
    const g = geom();
    const rect = svg.getBoundingClientRect();
    const py = (evt.clientY - rect.top) * (g.H / rect.height);
    const innerH = g.innerH;
    const hz = FMAX * (MARGIN.top + innerH - py) / innerH;
    const cand = nearestCandidate(f, hz, null);
    if (!cand) return;
    state.pathPicked = state.pathPicked || {};
    state.pathPicked[f] = cand.id;
    state.pathRank = state.pathRank || 2;
    $("hint").textContent =
      `已选 ${Object.keys(state.pathPicked).length} 帧; 双击空白处或按“提交路径”完成重选。`;
    renderPathPicked();
  });

  svg.addEventListener("dblclick", () => {
    if (state.tool === "path" && state.path) submitPath();
  });
}

function renderPathPicked() {
  // 简单刷新即可, 点已记录
  render();
  const svg = $("chart");
  const g = geom();
  const picked = state.pathPicked || {};
  Object.keys(picked).forEach((fStr) => {
    const f = +fStr;
    const cs = candidatesAt(state.gen, f);
    const c = cs.find((x) => x.id === picked[f]);
    if (!c) return;
    svg.appendChild(el("circle", {
      cx: g.x(f), cy: g.y(c.frequency), r: 7,
      fill: "none", stroke: "#5ad1ff", "stroke-width": 2,
    }));
  });
}

async function submitPath() {
  if (!state.path || !state.pathPicked || !Object.keys(state.pathPicked).length) {
    state.path = null; state.pathPicked = null; render(); return;
  }
  const a = Math.min(state.path.start, state.path.end);
  const b = Math.max(state.path.start, state.path.end);
  const rank = +($("pathRank") && $("pathRank").value) || state.pathRank || 2;
  const picked = {};
  Object.keys(state.pathPicked).forEach((k) => { picked[k] = state.pathPicked[k]; });
  await submitOp({
    utteranceId: state.uttId, gen: state.gen, type: "reselect_path",
    rank,
    reselect: { startFrame: a, endFrame: b + 1, picked },
    note: `重选 ${RANK_NAMES[rank]} 路径 [${a},${b + 1}) 共 ${Object.keys(picked).length} 帧`,
  });
  state.path = null; state.pathPicked = null;
}

async function tryLockAt(frame) {
  // 找到与该帧相邻的元音边界(start 或 end), end 帧本身不属于元音。
  const vws = curView().vowels;
  let edge = null;
  let vv = null;
  for (const v of vws) {
    if (frame === v.start) { edge = "start"; vv = v; break; }
    if (frame === v.end) { edge = "end"; vv = v; break; }
  }
  if (!edge) {
    $("hint").textContent = "锁定边界: 请点击元音区间的 start 或 end 帧。";
    return;
  }
  const g = geom();
  const cs = candidatesAt(state.gen, frame);
  // 锁定该帧频率最低的候选(F1)作为边界锚点。
  const cand = cs.slice().sort((p, q) => p.frequency - q.frequency)[0];
  await submitOp({
    utteranceId: state.uttId, gen: state.gen, type: "lock_boundary",
    frame,
    lock: { frame, vowelId: vv.id, edge, candidateId: cand.id },
    note: `锁定 ${vv.label} ${edge} 边界 f${frame} @ ${cand.frequency.toFixed(0)}Hz`,
  });
}

async function submitOp(body) {
  try {
    await api("/api/operations", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    await reload();
  } catch (e) {
    $("hint").textContent = "操作被拒绝: " + prettyErr(e);
  }
}

function prettyErr(e) {
  try { return JSON.parse(e.message).error; } catch (_) { return e.message; }
}

async function reload() {
  state.data = await api("/api/state");
  render();
  renderSide();
}

function renderSide() {
  const v = curView();
  const gv = curGenView();

  // 可信度 + 校验
  let edited = 0, sumConf = 0, count = 0;
  gv.final.forEach((row) => row.forEach((p) => {
    if (p.rank) {
      count++;
      sumConf += p.confidence || 0;
      if (p.edited) edited++;
    }
  }));
  $("credInfo").textContent =
    `候选代 gen${state.gen}; 可见点 ${count}; 已修订 ${edited}; 平均可信度 ${(sumConf / Math.max(1, count)).toFixed(2)}`;

  const issues = $("issues");
  issues.innerHTML = "";
  (gv.issues || []).forEach((iss) => {
    const d = document.createElement("div");
    d.className = "issue" + (iss.kind === "cross_gap_swap" ? " warn" : "");
    d.textContent = (iss.kind === "cross_gap_swap" ? "跨缺帧身份疑似交换: " : "严格递增违规: ")
      + `帧 ${iss.frame}` + (iss.rank ? ` ${RANK_NAMES[iss.rank] || ""}` : "")
      + " — " + iss.message
      + (iss.detail ? `(偏差 ${iss.detail.toFixed(0)}Hz)` : "");
    issues.appendChild(d);
  });
  if (!(gv.issues || []).length) {
    const d = document.createElement("div");
    d.className = "issue warn";
    d.style.color = "var(--ok)";
    d.textContent = "口径检查通过: 同帧严格递增, 缺帧未插值, 无跨缺口换轨。";
    issues.appendChild(d);
  }

  // 操作列表
  const opsList = $("opsList");
  opsList.innerHTML = "";
  const ops = v.operations.filter((o) => o.gen === state.gen);
  if (!ops.length) {
    opsList.innerHTML = '<li style="color:#93a0b8">尚无修订</li>';
  }
  ops.forEach((o) => {
    const li = document.createElement("li");
    let desc = o.note || o.type;
    li.innerHTML = `<span class="mono">${o.id.slice(-6)}</span> gen${o.gen} ${desc} `;
    const del = document.createElement("button");
    del.textContent = "撤销";
    del.onclick = async () => {
      await api("/api/operations/" + o.id, { method: "DELETE" });
      await reload();
    };
    li.appendChild(del);
    opsList.appendChild(li);
  });

  // 冲突
  const cl = $("conflictList");
  cl.innerHTML = "";
  const confs = v.conflicts || [];
  if (!confs.length) cl.innerHTML = '<li style="color:#93a0b8">无冲突</li>';
  confs.forEach((c) => {
    const li = document.createElement("li");
    li.className = "conflict-item";
    const tagCls = c.resolved ? "resolved" : c.type;
    const label = c.resolved ? "已解决" : (c.type === "ambiguous" ? "歧义(两项)" : "无匹配");
    const rankTxt = c.rank ? " " + RANK_NAMES[c.rank] : " 边界";
    li.innerHTML = `<span class="tag ${tagCls}">${label}</span>` +
      `帧 ${c.frame}${rankTxt} 旧 ${c.oldFrequency.toFixed(0)}Hz`;
    if (!c.resolved && c.candidateIds && c.candidateIds.length) {
      const sel = document.createElement("select");
      c.candidateIds.forEach((cid) => {
        const cand = candidatesAt(c.toGen || state.gen, c.frame)
          .find((x) => x.id === cid);
        const opt = document.createElement("option");
        opt.value = cid;
        opt.textContent = cand ? cand.frequency.toFixed(0) + "Hz" : cid;
        sel.appendChild(opt);
      });
      const ok = document.createElement("button");
      ok.textContent = "采用该候选";
      ok.onclick = async () => {
        await api("/api/conflicts/resolve", {
          method: "POST", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            utteranceId: state.uttId, toGen: 2,
            picks: { [c.id]: sel.value },
          }),
        });
        await reload();
      };
      li.appendChild(sel);
      li.appendChild(ok);
    }
    cl.appendChild(li);
  });
}

async function loadRuns() {
  const runs = await api("/api/runs");
  const ul = $("runsList");
  ul.innerHTML = "";
  runs.slice(0, 20).forEach((r) => {
    const li = document.createElement("li");
    li.textContent = `${r.kind} — ${r.detail} (${r.createdAt.replace("T", " ").slice(0, 19)})`;
    ul.appendChild(li);
  });
}

function setupControls() {
  document.querySelectorAll(".tool").forEach((b) => {
    b.onclick = () => {
      document.querySelectorAll(".tool").forEach((x) => x.classList.remove("active"));
      b.classList.add("active");
      state.tool = b.dataset.tool;
      state.path = null; state.pathPicked = null;
      const showPath = state.tool === "path";
      $("pathRankWrap").style.display = showPath ? "" : "none";
      $("pathSubmit").style.display = showPath ? "" : "none";
      render();
    };
  });
  $("genSelect").onchange = async (e) => {
    state.gen = +e.target.value;
    render(); renderSide();
  };
  $("uttSelect").onchange = async (e) => {
    state.uttId = e.target.value;
    const gc = 2;
    $("genSelect").options.length = 0;
    const maxGen = state.data.view[state.uttId].gens.reduce((m, x) => Math.max(m, x.gen), 1);
    for (let g = 1; g <= maxGen; g++) {
      const opt = document.createElement("option");
      opt.value = g; opt.textContent = "gen" + g + (g === 1 ? " 原算法" : " 重算候选");
      $("genSelect").appendChild(opt);
    }
    state.gen = 1;
    $("genSelect").value = "1";
    render(); renderSide();
  };
  $("pathSubmit").onclick = async () => { await submitPath(); };
  if ($("pathRank")) {
    $("pathRank").onchange = (e) => { state.pathRank = +e.target.value; };
  }
  $("replayBtn").onclick = async () => {
    await api("/api/replay", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ utteranceId: state.uttId, fromGen: 1, toGen: 2 }),
    });
    state.gen = 2;
    $("genSelect").value = "2";
    await reload();
    await loadRuns();
  };
  $("reimportBtn").onclick = async () => {
    if (!confirm("清空数据库并重新导入固定 fixture? 当前修订将被清除。")) return;
    await api("/api/admin/reimport", { method: "POST" });
    await reload();
    await loadRuns();
  };
}

function fillUttSelect() {
  const sel = $("uttSelect");
  sel.innerHTML = "";
  state.data.speakers.forEach((sp) => {
    state.data.utterances.filter((u) => u.speakerId === sp.id).forEach((u) => {
      const opt = document.createElement("option");
      opt.value = u.id;
      opt.textContent = `${sp.name} / ${u.label}`;
      sel.appendChild(opt);
    });
  });
  sel.value = state.uttId;
}

async function init() {
  setupPointer();
  setupControls();
  await reload();
  fillUttSelect();
  await loadRuns();
  window.addEventListener("resize", render);
}
init();
