'use strict';
const SVGNS='http://www.w3.org/2000/svg';
const BAND_COLORS=['var(--f1)','var(--f2)','var(--f3)','var(--f4)'];
const BAND_HEX=['#ff6b6b','#ffd166','#06d6a0','#7aa2ff'];
const FMAX=4000, MARGIN={l:54,r:14,t:26,b:28};
let state=null, mode='drag', pathSel=null, currentSvg=null;
const $=id=>document.getElementById(id);
const qs=s=>document.querySelector(s);

function toast(msg,isErr){
  const t=$('toast'); t.textContent=msg;
  t.style.borderColor=isErr?'#a33':'var(--line)';
  t.style.color=isErr?'#ffb3b3':'var(--fg)';
  t.style.display='block'; clearTimeout(toast._h);
  toast._h=setTimeout(()=>t.style.display='none',3200);
}
async function api(path,body){
  const opt=body?{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify(body)}:{};
  const r=await fetch(path,opt);
  const j=await r.json().catch(()=>({}));
  if(!r.ok) throw new Error(j.error||('HTTP '+r.status));
  return j;
}
function curTarget(){return {speaker:$('speakerSel').value,utter:$('utterSel').value}};
function q(obj){return new URLSearchParams(obj).toString()}

async function loadState(){
  const t=curTarget();
  try{
    state=await api('/api/state?'+q(t));
  }catch(e){toast('加载失败: '+e.message,true);return}
  pathSel=null;
  render();
}

function frameMeta(f){return state.frames.find(x=>x.frame===f)}
function cell(f,b){return state.final_cells.find(c=>c.frame===f&&c.band===b)}
function candsAt(f,b){return state.candidates.filter(c=>c.frame===f&&c.band===b)}
function candById(id){return state.candidates.find(c=>c.id===id)}

function geom(){
  const inWin=state.frames.filter(f=>f.in_window);
  const fmin=inWin[0].frame, fmax=inWin[inWin.length-1].frame;
  const fw=26, fh=420;
  const W=MARGIN.l+MARGIN.r+(fmax-fmin+1)*fw;
  const H=MARGIN.t+MARGIN.b+fh;
  return {fmin,fmax,fw,fh,W,H,
    x:f=>MARGIN.l+(f-fmin)*fw+fw/2,
    y:hz=>MARGIN.t+fh*(1-hz/FMAX)};
}

function el(name,attrs,parent){
  const e=document.createElementNS(SVGNS,name);
  for(const k in attrs) e.setAttribute(k,attrs[k]);
  if(parent) parent.appendChild(e);
  return e;
}

function render(){
  renderHeader();
  renderSvg();
  renderIssues();
  renderConflicts();
  renderOps();
  renderLog();
}

function renderHeader(){
  const u=state.utterance, sp=state.speaker;
  $('setTag').textContent=state.active_set;
  $('metaLine').textContent=
    `${sp.name} · ${u.id}（${u.pronunciation_version}） · ${u.fps}fps · 窗[${u.window_from_frame}, ${u.window_to_frame})`;
}

function renderSvg(){
  const host=$('svgHost'); host.innerHTML='';
  const g=geom();
  const svg=el('svg',{width:g.W,height:g.H,viewBox:`0 0 ${g.W} ${g.H}`},host);
  currentSvg=svg;
  drawVowels(svg,g);
  drawSpectrum(svg,g); drawAxes(svg,g);
  drawGapShade(svg,g);
  drawCandidates(svg,g); drawTracks(svg,g); drawLocks(svg,g);
}

function drawSpectrum(svg,g){
  // one rect per (frame,bin); bins grouped into 8 vertical stripes per frame
  const byFrame={};
  for(const s of state.spectrum){(byFrame[s.frame]??=[]).push(s)}
  for(const fStr in byFrame){
    const f=+fStr;
    const bins=byFrame[f];
    for(const b of bins){
      const y=g.y(b.freq_hi), y2=g.y(b.freq_lo);
      const alpha=(b.energy*0.85).toFixed(3);
      el('rect',{x:g.x(f)-g.fw/2+1,y,width:g.fw-2,height:Math.max(1,y2-y),
        fill:`rgba(125,180,255,${alpha})`},svg);
    }
  }
}

function drawAxes(svg,g){
  for(let hz=0;hz<=FMAX;hz+=500){
    const y=g.y(hz);
    el('line',{x1:MARGIN.l,y1:y,x2:g.W-MARGIN.r,y2:y,
      stroke:'#202736','stroke-width':1},svg);
    const t=el('text',{x:6,y:y+3},svg); t.textContent=hz;
  }
  for(let f=g.fmin;f<=g.fmax;f++){
    const x=g.x(f);
    el('line',{x1:x,y1:MARGIN.t,x2:x,y2:MARGIN.t+g.fh,
      stroke:'#1a2030','stroke-width':1},svg);
    if(f%2===0){const t=el('text',{x:x-6,y:g.H-8},svg);t.textContent=f}
  }
  const t=el('text',{x:MARGIN.l,y:14},svg);
  t.textContent='频率 Hz / 帧序号';
}

function drawVowels(svg,g){
  for(const v of state.vowels){
    const x1=g.x(v.start_frame), x2=g.x(v.end_frame-1)+g.fw/2;
    el('rect',{x:x1,y:MARGIN.t,width:x2-x1,height:g.fh,
      fill:'rgba(125,180,255,0.10)'},svg);
    el('line',{x1:x1,y1:MARGIN.t,x2:x1,y2:MARGIN.t+g.fh,
      stroke:'rgba(125,180,255,0.5)','stroke-dasharray':'3 3'},svg);
    const t=el('text',{x:x1+4,y:MARGIN.t+12,class:'vlabel'},svg);
    t.textContent=v.label;
  }
}

function drawGapShade(svg,g){
  // missing frames (in window, not present) and outside-window tails
  for(const fm of state.frames){
    if(!fm.in_window) continue;
    if(fm.present) continue;
    el('rect',{x:g.x(fm.frame)-g.fw/2,y:MARGIN.t,width:g.fw,height:g.fh,
      fill:'rgba(0,0,0,0.55)'},svg);
    el('line',{x1:g.x(fm.frame)-g.fw/2,y1:MARGIN.t,
      x2:g.x(fm.frame)-g.fw/2,y2:MARGIN.t+g.fh,
      stroke:'#555','stroke-dasharray':'2 2'},svg);
    const t=el('text',{x:g.x(fm.frame)-14,y:MARGIN.t+g.fh/2},svg);
    t.textContent='缺帧';
  }
}

function drawCandidates(svg,g){
  // Every raw algorithm candidate stays visible: small hollow rings, dim if
  // not the current effective choice.
  for(const c of state.candidates){
    const fm=frameMeta(c.frame);
    if(!fm||!fm.in_window||!fm.present) continue;
    const cellNow=cell(c.frame,c.band);
    const chosen=cellNow && cellNow.candidate_id===c.id;
    el('circle',{cx:g.x(c.frame),cy:g.y(c.freq),r:chosen?3.4:4.2,
      fill:chosen?BAND_HEX[c.band]:'none',
      stroke:chosen?BAND_HEX[c.band]:'#6b7891',
      'stroke-width':chosen?1.2:1,
      opacity:chosen?1:0.75,
      'data-cand':c.id, class:'cand-ring'},svg);
  }
}

function drawTracks(svg,g){
  // Broken polylines: segments never span a missing/non-present frame gap.
  for(let b=0;b<4;b++){
    let pts=[];
    const flush=()=>{
      if(pts.length<2) {pts=[];return}
      el('polyline',{points:pts.map(p=>`${p.x},${p.y}`).join(' '),
        fill:'none',stroke:BAND_HEX[b],'stroke-width':2.2,
        'stroke-linejoin':'round'},svg);
      pts=[];
    };
    for(let f=g.fmin;f<=g.fmax;f++){
      const fm=frameMeta(f);
      const c=cell(f,b);
      if(!fm||!fm.in_window||!fm.present||!c||c.freq===0){flush();continue}
      const p={x:g.x(f),y:g.y(c.freq)}; pts.push(p);
      drawPoint(svg,g,f,b,c);
    }
    flush();
    // label at first visible point
    const first=state.final_cells.find(c=>c.band===b&&c.present&&c.in_window&&c.freq>0);
    if(first){const t=el('text',{x:g.x(first.frame)-18,y:g.y(first.freq)-8,
      fill:BAND_HEX[b]},svg);t.textContent='F'+(b+1)}
  }
}

function drawPoint(svg,g,f,b,c){
  const isAuto=c.source==='auto';
  const ring=el('circle',{cx:g.x(f),cy:g.y(c.freq),r:5.5,
    fill:BAND_HEX[b],stroke:'#0c0f15','stroke-width':1.5,
    class:'track-point',
    'data-frame':f,'data-band':b,'data-cand':c.candidate_id,
    style:'cursor:pointer'},svg);
  // confidence halo for low confidence auto points
  if(isAuto&&c.confidence<0.7){
    el('circle',{cx:g.x(f),cy:g.y(c.freq),r:8.5,
      fill:'none',stroke:'#ff9f43','stroke-width':1,
      'stroke-dasharray':'2 2',opacity:0.8},svg);
  }
  ring.addEventListener('mouseenter',()=>pointTip(f,b,c,true));
  ring.addEventListener('mouseleave',()=>pointTip(f,b,c,false));
  ring.addEventListener('click',(ev)=>onPointClick(ev,f,b));
}

function pointTip(f,b,c,show){
  let tip=$('tip');
  if(!show){const old=document.getElementById('tip');if(old)old.remove();return}
  const old=document.getElementById('tip');if(old)old.remove();
  tip=el('g',{id:'tip'});
  const g0=geom();
  const x=g0.x(f)+10,y=g0.y(c.freq)-24;
  el('rect',{x,y,width:150,height:34,rx:5,
    fill:'#0d1322',stroke:'#39445c'});
  const t1=el('text',{x:x+6,y:y+14,fill:'#dbe4f5'});
  t1.textContent=`帧${f} F${b+1} ${c.freq.toFixed(0)}Hz`;
  const t2=el('text',{x:x+6,y:y+28,fill:'#93a0b5'});
  t2.textContent=`来源:${sourceLabel(c.source)} 可信度 ${(c.confidence*100|0)}%`;
  currentSvg.appendChild(tip);
}

function sourceLabel(s){return {auto:'自动',move:'拖动',path:'路径',lock:'锁定'}[s]||s}

function drawLocks(svg,g){
  for(const op of (state.operations||[])){
    if(op.kind!=='lock')continue;
    for(const it of op.items){
      const c=cell(it.frame,it.band);
      if(!c)continue;
      el('circle',{cx:g.x(it.frame),cy:g.y(c.freq),r:8.5,
        fill:'none',stroke:'#ffd166','stroke-width':2},svg);
    }
  }
}

let activePoint=null; // {frame,band} waiting for target candidate in drag mode

function onPointClick(ev,f,b){
  ev.stopPropagation();
  if(mode==='drag'){
    activePoint={frame:f,band:b};
    highlightCandidates(f,b);
    toast(`帧 ${f} F${b+1}：请点击同帧同带的空心候选以吸附（再点轨迹点取消）`);
  } else if(mode==='path'){
    if(!pathSel){
      pathSel={band:b,start:f,end:null};
      toast(`已选路径起点 帧 ${f} F${b+1}，请点击同带终点帧`);
    } else if(b===pathSel.band && f!==pathSel.start){
      pathSel.end=f; submitPath();
    } else {
      pathSel={band:b,start:f,end:null};
      toast(`重新选择：起点 帧 ${f} F${b+1}`);
    }
  } else if(mode==='lock'){
    submitLock(f,b);
  }
}

function highlightCandidates(f,b){
  document.querySelectorAll('.cand-ring').forEach(r=>{
    const cf=+r.dataset.cand, c=candById(cf);
    if(c.frame===f&&c.band===b){
      r.setAttribute('stroke-width',3);
      r.setAttribute('stroke','#ffffff');
      r.style.cursor='pointer';
    } else {
      r.setAttribute('stroke-width',r.getAttribute('fill')==='none'?1:1.2);
    }
  });
}

document.addEventListener('click',async ev=>{
  const ring=ev.target.closest('.cand-ring');
  if(ring && activePoint){
    ev.stopPropagation();
    const c=candById(+ring.dataset.cand);
    if(c.frame!==activePoint.frame||c.band!==activePoint.band){
      toast('只能吸附到同帧同带候选',true); return;
    }
    const t=curTarget();
    try{
      await api('/api/operations',{speaker:t.speaker,utter:t.utter,
        kind:'move',note:`拖动 帧${c.frame} F${c.band+1} -> ${c.freq.toFixed(0)}Hz`,
        items:[{frame:c.frame,band:c.band,candidate_id:c.id,freq:c.freq}]});
      toast('已保存拖动修订'); activePoint=null; await loadState();
    }catch(e){toast(e.message,true)}
    return;
  }
  if(!ev.target.closest('.track-point') && !ring){activePoint=null}
});

async function submitPath(){
  const {band,start,end}=pathSel;
  const lo=Math.min(start,end), hi=Math.max(start,end);
  const items=[];
  for(let f=lo;f<=hi;f++){
    const fm=frameMeta(f);
    if(!fm.in_window||!fm.present){
      toast(`帧 ${f} 不可见，路径必须落在连续可见帧`,true); pathSel=null; return;
    }
    // In path mode the user commits to the default candidate of each frame;
    // ambiguous frames are handled by dragging individual points afterwards.
    const cs=candsAt(f,band);
    const chosen=cs.find(x=>{const cc=cell(f,band);return cc&&x.id===cc.candidate_id})||cs[0];
    items.push({frame:f,band,candidate_id:chosen.id,freq:chosen.freq});
  }
  const t=curTarget();
  try{
    await api('/api/operations',{speaker:t.speaker,utter:t.utter,
      kind:'path',note:`重选路径 帧${lo}..${hi} F${band+1}`,items});
    toast(`已保存路径修订（帧 ${lo}..${hi} F${band+1}）`);
    pathSel=null; await loadState();
  }catch(e){toast(e.message,true);pathSel=null;await loadState()}
}

async function submitLock(f,b){
  const c=cell(f,b);
  if(!c||!c.candidate_id){toast('该帧无可用点',true);return}
  const t=curTarget();
  try{
    await api('/api/operations',{speaker:t.speaker,utter:t.utter,
      kind:'lock',note:`锁定边界帧 帧${f} F${b+1}`,
      items:[{frame:f,band,candidate_id:c.candidate_id,freq:c.freq}]});
    toast(`已锁定 帧 ${f} F${b+1}`); await loadState();
  }catch(e){toast(e.message,true)}
}

document.querySelectorAll('.tools button[data-mode]').forEach(btn=>{
  btn.addEventListener('click',()=>{
    document.querySelectorAll('.tools button[data-mode]').forEach(x=>x.classList.remove('active'));
    btn.classList.add('active'); mode=btn.dataset.mode; pathSel=null; activePoint=null;
    $('modeHint').textContent={
      drag:'先点彩色轨迹点，再点同帧同带空心候选完成吸附。',
      path:'点击同一带的起点与终点，提交连续帧候选路径。',
      lock:'点击轨迹点，将该边界帧锁定为身份锚点。'}[mode];
  });
});

function renderIssues(){
  const box=$('issueBox'); box.innerHTML='';
  if(!(state.issues||[]).length){
    box.innerHTML='<span class="ok">✓ 当前轨迹满足同帧严格递增，且无跨缺帧身份交换。</span>';
    return;
  }
  for(const i of state.issues){
    const d=document.createElement('div'); d.className='issue';
    if(i.code==='not_strictly_increasing')
      d.textContent=`⚠ 帧 ${i.frame}：${i.message}`;
    else
      d.textContent=`⚠ 跨不可见帧：${i.message}（帧 ${i.frame} → ${i.frame2}）`;
    box.appendChild(d);
  }
}

function refCandOptions(ref){
  return (ref.options||[]).map(id=>candById(id)).filter(Boolean);
}

function renderConflicts(){
  const box=$('conflictBox'); box.innerHTML='';
  if(!(state.conflicts||[]).length){
    box.innerHTML='<div class="muted">暂无</div>';
  } else {
    for(const r of (state.conflicts||[])){
      const d=document.createElement('div'); d.className='conflict';
      const opts=refCandOptions(r);
      d.innerHTML=`<div>操作 #${r.from_op_id} · 帧 ${r.frame} F${r.band+1}
        · 旧候选 ${r.old_candidate_id}：${opts.length} 个候选落在 ±40Hz，系统不替你选择</div>`;
      opts.forEach(c=>{
        const b=document.createElement('button');
        b.textContent=`${c.freq.toFixed(0)}Hz (候选 ${c.id})`;
        b.onclick=async()=>{
          const t=curTarget();
          try{
            await api('/api/conflicts/resolve',{speaker:t.speaker,utter:t.utter,
              from_op_id:r.from_op_id,frame:r.frame,band:r.band,candidate_id:c.id});
            toast('冲突已按你的选择消解');await loadState();
          }catch(e){toast(e.message,true)}
        };
        d.appendChild(b);
      });
      box.appendChild(d);
    }
  }
  const ub=$('unresolvedBox'); ub.innerHTML='';
  if((state.unresolved||[]).length){
    ub.innerHTML='<div class="muted">无法映射（候选消失，未强行选择）：</div>';
    for(const r of (state.unresolved||[])){
      const d=document.createElement('div'); d.className='issue';
      d.textContent=`· 操作 #${r.from_op_id} 帧 ${r.frame} F${r.band+1}（候选 ${r.old_candidate_id}）`;
      ub.appendChild(d);
    }
  }
}

function renderOps(){
  const tb=document.querySelector('#opsTable tbody'); tb.innerHTML='';
  const ops=[...(state.operations||[])].reverse();
  for(const op of ops){
    const tr=document.createElement('tr');
    const desc=op.items.map(i=>`帧${i.frame}/F${i.band+1}`).join(' ');
    tr.innerHTML=`<td>${op.id}</td><td><span class="pill ${op.kind}">${
      {move:'拖动',path:'路径',lock:'锁定'}[op.kind]||op.kind}</span></td>
      <td>${op.cand_version}</td><td>${desc}</td><td>${op.author}</td>`;
    tb.appendChild(tr);
  }
}

function renderLog(){
  const box=$('logBox'); box.innerHTML='';
  for(const l of (state.run_log||[]).slice().reverse()){
    const d=document.createElement('div');
    d.textContent=`${l.at.slice(11)} [${l.actor}] ${l.action} ${l.detail||''}`;
    box.appendChild(d);
  }
}

async function initSelectors(){
  // speakers/utterances come from a state call with defaults first.
  const st=await api('/api/state?speaker=s1&utter=ai-a');
  const sps=[]; const us=[];
  // derive lists from endpoints
  const states=await Promise.all([
    api('/api/state?speaker=s1&utter=ai-a'),
    api('/api/state?speaker=s2&utter=ai-a').catch(()=>null),
    api('/api/state?speaker=s1&utter=ai-b').catch(()=>null),
  ]);
  const spSel=$('speakerSel'), uSel=$('utterSel');
  spSel.innerHTML=''; uSel.innerHTML='';
  const spMap={};
  states.filter(Boolean).forEach(s=>{spMap[s.speaker.id]=s.speaker.name});
  Object.entries(spMap).forEach(([id,name])=>{
    const o=document.createElement('option');o.value=id;o.textContent=name;spSel.appendChild(o);
  });
  function fillUtter(sp){
    uSel.innerHTML='';
    states.filter(s=>s&&s.speaker.id===sp).forEach(s=>{
      const o=document.createElement('option');
      o.value=s.utterance.id;
      o.textContent=`${s.utterance.id} (${s.utterance.pronunciation_version})`;
      uSel.appendChild(o);
    });
  }
  fillUtter('s1');
  spSel.onchange=()=>{fillUtter(spSel.value);loadState()};
  uSel.onchange=loadState;
  await loadState();
}

$('rerunBtn').onclick=async()=>{
  const t=curTarget();
  try{
    const r=await api('/api/rerun',t);
    toast(`重映射完成：唯一恢复 ${r.restored_ops} 个操作，冲突 ${r.conflict_count}，无法映射 ${r.unresolved_count}`);
    await loadState();
  }catch(e){toast(e.message,true)}
};
$('undoBtn').onclick=async()=>{
  try{await api('/api/undo',curTarget());toast('已撤销');await loadState()}
  catch(e){toast(e.message,true)}
};
$('exportBtn').onclick=()=>{window.location='/api/export?' + q(curTarget())};
$('resetBtn').onclick=async()=>{
  if(!confirm('清空数据库并重新导入固定 fixture？'))return;
  await api('/api/reset',{}); toast('已重置'); location.reload();
};
$('importBtn').onclick=()=>$('importFile').click();
$('importFile').onchange=async ev=>{
  const file=ev.target.files[0]; if(!file)return;
  const r=await fetch('/api/import',{method:'POST',
    headers:{'Content-Type':'application/json'},body:await file.text()});
  const j=await r.json();
  if(!r.ok){toast(j.error||'导入失败',true);return}
  toast(`已导入：${j.speakers} 说话人 / ${j.candidates} 候选 / ${j.operations} 操作`);
  setTimeout(()=>location.reload(),600);
};

initSelectors().catch(e=>toast('初始化失败: '+e.message,true));
