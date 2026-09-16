/* SOP model helpers + infinite-canvas flowchart editor (pan/zoom, drag, connect, palette, minimap) */
(function(){
  const F={zoom:{},view:{},multi:{}};
  const TYPES={
    start:{label:'Start',icon:'circle'},question:{label:'Question',icon:'help'},evidence:{label:'Evidence',icon:'camera'},
    decision:{label:'Decision',icon:'split'},approval:{label:'Approval',icon:'check'},wait:{label:'Wait',icon:'clock'},
    repeat:{label:'Repeat check',icon:'repeat'},child:{label:'Child SOP',icon:'workflow'},parallel:{label:'Parallel',icon:'split'},
    join:{label:'Join',icon:'merge'},action:{label:'Task',icon:'check'},end:{label:'End',icon:'flag'}};
  F.TYPES=TYPES;
  const ANSWERS={text:'Text',number:'Number',choice:'Single choice',multi:'Multiple choice',scan:'RFID scan',date:'Date',ref:'Pick from list'};
  F.ANSWERS=ANSWERS;
  const REFS={pens:'Pens',vendors:'Vendors',trucks:'Trucks',parks:'Parks',items:'Items',people:'People'};
  F.REFS=REFS;

  F.byId=nodes=>{const m={};nodes.forEach(n=>m[n.id]=n);return m;};
  F.stepCount=nodes=>nodes.filter(n=>!['start','end','join','parallel'].includes(n.type)).length;
  F.pages=nodes=>[...new Set(nodes.map(n=>n.page).filter(Boolean))];
  F.nid=nodes=>{let i=nodes.length+1;const ids=new Set(nodes.map(n=>n.id));while(ids.has('n'+i))i++;return 'n'+i;};

  /* ---------- geometry constants ---------- */
  const NW=264, GRID=20, RH=48, CW=320;
  const PILL=t=>t==='start'||t==='end'||t==='join';
  const estH=n=>PILL(n.type)?60:100;
  const snap=v=>Math.round(v/GRID)*GRID;

  /* topological rank (longest path, back-edges ignored) + lane columns, top-down */
  F.layout=function(nodes){
    const m=F.byId(nodes); const start=nodes.find(n=>n.type==='start')||nodes[0];
    const order=[],seen=new Set(),onStack=new Set(),back=new Set();
    const dfs=id=>{if(!m[id]||seen.has(id))return;seen.add(id);onStack.add(id);
      (m[id].next||[]).forEach(e=>{if(onStack.has(e.to))back.add(id+'>'+e.to);else dfs(e.to);});onStack.delete(id);order.unshift(id);};
    if(start)dfs(start.id); nodes.forEach(n=>dfs(n.id));
    const rank={}; order.forEach(id=>{if(rank[id]==null)rank[id]=0;(m[id].next||[]).forEach(e=>{if(!m[e.to]||back.has(id+'>'+e.to))return;rank[e.to]=Math.max(rank[e.to]||0,rank[id]+1);});});
    const col={};
    order.forEach(id=>{if(col[id]==null)col[id]=0;const outs=(m[id].next||[]).filter(e=>m[e.to]&&!back.has(id+'>'+e.to));
      const uniq=[...new Set(outs.map(e=>e.to))];
      uniq.forEach((to,i)=>{if(col[to]!=null)return;const spread=uniq.length>1?i-(uniq.length-1)/2:0;col[to]=col[id]+spread;});});
    nodes.forEach(n=>{if(n.type==='end'&&n.outcome==='rejected'){const preds=nodes.filter(p=>(p.next||[]).some(e=>e.to===n.id));const pc=preds.length?Math.max(...preds.map(p=>col[p.id]||0)):0;col[n.id]=Math.max(col[n.id]||0,pc+1);}});
    // resolve collisions per rank
    const byRank={}; order.forEach(id=>(byRank[rank[id]]=byRank[rank[id]]||[]).push(id));
    const pos={}; let y=40; let prevGroups=null;
    Object.keys(byRank).map(Number).sort((a,b)=>a-b).forEach(r=>{
      const ids=byRank[r].sort((a,b)=>col[a]-col[b]); const used=[];
      ids.forEach(id=>{let c=col[id];while(used.some(u=>Math.abs(u-c)<1))c+=1;used.push(c);col[id]=c;});
      const groups=new Set(ids.map(id=>m[id].page||m[id].lane||''));
      if(prevGroups&&[...groups].some(g=>g&&!prevGroups.has(g)))y+=60;
      let h=0; ids.forEach(id=>{pos[id]={x:snap(col[id]*CW),y:snap(y)};h=Math.max(h,estH(m[id]));});
      y+=h+RH; prevGroups=groups;
    });
    return {pos,rank,order};
  };

  /* positions: persisted sop.layout wins; missing nodes are auto-placed */
  F.positions=function(sop){
    const saved=sop.layout||{}; const ids=Object.keys(saved).filter(id=>sop.nodes.some(n=>n.id===id));
    if(!ids.length)return F.layout(sop.nodes).pos;
    const pos={}; ids.forEach(id=>pos[id]={x:saved[id].x,y:saved[id].y});
    const auto=F.layout(sop.nodes).pos; const m=F.byId(sop.nodes);
    const free=(x,y)=>!Object.values(pos).some(p=>Math.abs(p.x-x)<NW&&Math.abs(p.y-y)<100);
    let guard=0,changed=true;
    while(changed&&guard++<50){changed=false;
      sop.nodes.forEach(n=>{if(pos[n.id])return;
        const pred=sop.nodes.find(p=>pos[p.id]&&(p.next||[]).some(e=>e.to===n.id));
        const succ=sop.nodes.find(s=>pos[s.id]&&(n.next||[]).some(e=>e.to===s.id));
        let x,y;
        if(pred){x=pos[pred.id].x;y=pos[pred.id].y+estH(pred)+RH;}
        else if(succ&&guard>1){x=pos[succ.id].x+CW;y=pos[succ.id].y;}
        else if(guard>2){const b=bounds(pos,m);x=b.x1+60;y=b.y0;}
        else return;
        let tries=0;while(!free(x,y)&&tries++<20)x+=CW;
        pos[n.id]={x:snap(x),y:snap(y)};changed=true;});}
    sop.nodes.forEach(n=>{if(!pos[n.id])pos[n.id]=auto[n.id]||{x:0,y:0};});
    return pos;
  };
  function bounds(pos,m,hs){
    const ids=Object.keys(pos).filter(id=>!m||m[id]); if(!ids.length)return {x0:0,y0:0,x1:NW,y1:100};
    let x0=1e9,y0=1e9,x1=-1e9,y1=-1e9;
    ids.forEach(id=>{const p=pos[id];const h=(hs&&hs[id])||(m&&m[id]?estH(m[id]):100);x0=Math.min(x0,p.x);y0=Math.min(y0,p.y);x1=Math.max(x1,p.x+NW);y1=Math.max(y1,p.y+h);});
    return {x0,y0,x1,y1};
  }

  F.evalDecision=function(n,answers,ctx){
    const a=answers[n.q];
    if(n.op==='duplicate')return ctx&&ctx.prevScans?ctx.prevScans.includes(a):false;
    if(a===undefined||a===''||a===null)return undefined;
    let v=n.value; if(typeof v==='string'&&v.startsWith('@'))v=answers[v.slice(1)];
    if(n.op==='=')return String(a)===String(v);
    if(n.op==='≠')return String(a)!==String(v);
    if(n.op==='<')return Number(a)<Number(v);
    if(n.op==='>')return Number(a)>Number(v);
    return undefined;
  };
  F.rejectHit=function(n,val){
    const r=n.reject; if(!r||val===undefined||val===''||val===null)return false;
    if(r.op==='cannot')return val==='__cannot';
    if(r.op==='=')return String(val)===String(r.value);
    if(r.op==='<')return Number(val)<Number(r.value);
    if(r.op==='>')return Number(val)>Number(r.value);
    return false;
  };

  F.validate=function(sop){
    const nodes=sop.nodes,m=F.byId(nodes),iss=[];
    const start=nodes.find(n=>n.type==='start'); if(!start)iss.push({msg:'No start step'});
    const seen=new Set(); const st=start?[start.id]:[];
    while(st.length){const id=st.pop();if(seen.has(id)||!m[id])continue;seen.add(id);(m[id].next||[]).forEach(e=>st.push(e.to));}
    nodes.forEach(n=>{
      const L=n.label||F.TYPES[n.type].label;
      if(!seen.has(n.id))iss.push({id:n.id,msg:L+': not connected'});
      if(n.type!=='end'&&!(n.next||[]).length)iss.push({id:n.id,msg:L+': no next step'});
      (n.next||[]).forEach(e=>{if(!m[e.to])iss.push({id:n.id,msg:L+': broken link'});});
      if(n.type==='question'&&(n.answer==='choice'||n.answer==='multi')&&!(n.options||[]).length)iss.push({id:n.id,msg:L+': no options'});
      if(n.type==='decision'&&n.op!=='duplicate'&&(!n.q||!m[n.q]))iss.push({id:n.id,msg:L+': choose a question'});
      if(n.type==='decision'&&(n.next||[]).length<2)iss.push({id:n.id,msg:L+': needs Yes and No'});
      if(n.type==='approval'&&!S.get('roles',n.roleId))iss.push({id:n.id,msg:L+': choose approver role'});
      if(n.type==='child'&&!S.get('sops',n.sopId))iss.push({id:n.id,msg:L+': choose child SOP'});
      if(n.type==='child'&&n.sopId===sop.id)iss.push({id:n.id,msg:L+': cannot follow itself'});
    });
    if(!nodes.some(n=>n.type==='end'))iss.push({msg:'No end step'});
    return iss;
  };
  F.changed=function(sop){const v=sop.versions[sop.versions.length-1];return !v||JSON.stringify(v.nodes)!==JSON.stringify(sop.nodes);};
  F.latest=sop=>sop.versions[sop.versions.length-1];

  function badges(n,sop){
    const b=[]; const t=(x,tone)=>b.push(UI.tag(x,tone));
    const m=F.byId(sop.nodes);
    if(n.type==='question'){t(ANSWERS[n.answer]||n.answer,'mut');if(n.unit)t(n.unit,'mut');if(n.required)t('Required','mut');
      if(n.onlyIf&&m[n.onlyIf.q]){const q=m[n.onlyIf.q];const o=(q.options||[]).find(o=>o.v===n.onlyIf.v);t('If '+(o?o.l:n.onlyIf.v),'info');}
      if(n.reject)t('Reject: '+(n.reject.op==='cannot'?"can't answer":(((n.options||[]).find(o=>o.v===n.reject.value)||{}).l||n.reject.op+' '+n.reject.value)),'dng');}
    if(n.type==='evidence'){t((n.media||[]).map(x=>x[0].toUpperCase()+x.slice(1)).join('/'),'teal');t((n.min||0)+'–'+(n.max||1),'mut');}
    if(n.type==='approval')t(REG.labelById('roles',n.roleId)||'No role','pur');
    if(n.type==='wait'){const s=S.get('settings',n.setting);t((s?s.value:n.amount)+' '+(s?s.unit:n.unit),'warn');if(s)t('Setting','mut');}
    if(n.type==='repeat'){const e=S.get('settings',n.everySetting),f=S.get('settings',n.forSetting);t('Every '+(e?e.value:n.every)+' '+(n.everyUnit==='hours'?'h':n.everyUnit),'warn');t((f?f.value:n.forAmount)+' '+(n.forUnit||'days'),'warn');}
    if(n.type==='child'){const c=S.get('sops',n.sopId);c?t(F.stepCount(c.nodes)+' steps','ok'):t('Not set','warn');}
    if(n.type==='action'){const it=S.get('items',n.itemId);if(it)t(it.name,'teal');if(n.dose)t(n.dose+(it&&it.unit?' '+it.unit:'')+(n.per?'/'+n.per:'')+(n.route?' · '+n.route:''),'mut');}
    if(n.type==='decision'){const q=m[n.q];t(n.op==='duplicate'?'Duplicate scan':(q?'':'No question')+(n.op+' '+(String(n.value).startsWith('@')?(m[n.value.slice(1)]||{}).label:(((q&&q.options)||[]).find(o=>o.v===n.value)||{}).l||n.value)),'warn');}
    if(n.type==='end'&&n.outcome==='rejected')t('Rejected','dng');
    return b.join('');
  }
  function childLabel(n){if(n.type!=='child')return n.label;const c=S.get('sops',n.sopId);return c?c.title:n.label;}

  /* ---------- stylesheet (owned by this module) ---------- */
  (function(){if(document.querySelector('link[data-flow-css]'))return;const l=document.createElement('link');l.rel='stylesheet';l.href='styles/flow.css';l.dataset.flowCss='1';document.head.appendChild(l);})();

  /* ---------- rendering ---------- */
  const PALETTE=['question','evidence','decision','approval','wait','repeat','child','action','parallel','end'];
  const HINT={question:'Ask the operator',evidence:'Photo or video proof',decision:'Branch Yes / No',approval:'Role signs off',wait:'Pause for time',repeat:'Periodic check',child:'Run another SOP',action:'Do a task',parallel:'Split into lanes',end:'Finish the flow'};
  F._h={};
  const hOf=(sop,n)=>((F._h[sop.id]||{})[n.id])||estH(n);

  function view(sop){return F.view[sop.id]||(F.view[sop.id]={x:60,y:40,k:1,fresh:true});}

  function outPorts(n,sop){
    const nx=n.next||[];
    if(n.type==='end')return [];
    if((n.type==='decision'||n.type==='parallel')&&nx.length>=2)return nx.map((e,i)=>({i,x:NW*(i+1)/(nx.length+1),label:e.when||('Branch '+(i+1))}));
    return [{i:n.type==='parallel'?nx.length:0,x:NW/2,label:''}];
  }
  function edgeGeom(sop,pos,n,e,i){
    const a=pos[n.id],b=pos[e.to]; if(!a||!b)return null; const m=F.byId(sop.nodes);
    const ports=outPorts(n,sop); const pt=ports.find(p=>p.i===i)||ports[0]||{x:NW/2};
    const sx=a.x+pt.x, sy=a.y+hOf(sop,n), tx=b.x+NW/2, ty=b.y;
    let d, lx, ly;
    if(ty>sy+24){
      const my=sy+Math.max(18,Math.min(40,(ty-sy)/2));
      d=ortho([[sx,sy],[sx,my],[tx,my],[tx,ty]]); lx=sx; ly=sy+14;
    }else{
      const side=b.x>=a.x?1:-1; const hb=hOf(sop,m[e.to]);
      const outX=side>0?Math.max(a.x,b.x)+NW+40:Math.min(a.x,b.x)-40;
      const by=b.y+hb/2; const ex=side>0?b.x+NW:b.x;
      d=ortho([[sx,sy],[sx,sy+22],[outX,sy+22],[outX,by],[ex,by]]); lx=sx; ly=sy+14;
    }
    return {d,lx,ly};
  }
  function ortho(pts){
    const r=10; let d=`M${pts[0][0]},${pts[0][1]}`;
    for(let i=1;i<pts.length-1;i++){
      const [px,py]=pts[i-1],[cx,cy]=pts[i],[nx,ny]=pts[i+1];
      const d1=Math.hypot(cx-px,cy-py),d2=Math.hypot(nx-cx,ny-cy); const rr=Math.min(r,d1/2,d2/2);
      if(rr<1){d+=` L${cx},${cy}`;continue;}
      const ax=cx-(cx-px)/d1*rr, ay=cy-(cy-py)/d1*rr, bx=cx+(nx-cx)/d2*rr, by=cy+(ny-cy)/d2*rr;
      d+=` L${ax},${ay} Q${cx},${cy} ${bx},${by}`;
    }
    const l=pts[pts.length-1]; return d+` L${l[0]},${l[1]}`;
  }
  function edgesSvg(sop,pos,st){
    let out='';
    sop.nodes.forEach(n=>(n.next||[]).forEach((e,i)=>{
      const g=edgeGeom(sop,pos,n,e,i); if(!g)return;
      const key=n.id+':'+i; const sel=st&&st.edge===key;
      out+=`<path class="fx-wire ${sel?'sel':''}" d="${g.d}" marker-end="url(#fxah${sel?'s':''})"/><path class="fx-wire-hit" data-edge="${key}" d="${g.d}"/>`;
      if(e.when&&!(n.type==='decision'||n.type==='parallel')||e.when&&(n.next||[]).length<2){const w=e.when.length*6.2+14;out+=`<g class="fx-elabel"><rect x="${g.lx-w/2}" y="${g.ly-9}" width="${w}" height="18" rx="9"/><text x="${g.lx}" y="${g.ly+4}">${esc(e.when)}</text></g>`;}
    }));
    return `<defs><marker id="fxah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse"><path d="M0,1 L10,5 L0,9 z" class="fx-ah"/></marker><marker id="fxahs" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse"><path d="M0,1 L10,5 L0,9 z" class="fx-ah sel"/></marker></defs>`+out;
  }
  function groupsHtml(sop,pos){
    const groups={};
    sop.nodes.forEach(n=>{const g=n.page?'p:'+n.page:(n.lane?'l:'+n.lane:null);if(!g||!pos[n.id])return;(groups[g]=groups[g]||[]).push(n);});
    return Object.entries(groups).map(([g,ns])=>{
      const x0=Math.min(...ns.map(n=>pos[n.id].x))-20,y0=Math.min(...ns.map(n=>pos[n.id].y))-50,
        x1=Math.max(...ns.map(n=>pos[n.id].x+NW))+20,y1=Math.max(...ns.map(n=>pos[n.id].y+hOf(sop,n)))+20;
      return `<div class="fx-group ${g[0]==='p'?'page':'lane'}" style="left:${x0}px;top:${y0}px;width:${x1-x0}px;height:${y1-y0}px">
        <div class="fx-group-head" data-group="${esc(g)}" title="Drag to move the whole ${g[0]==='p'?'page':'lane'}">${ic(g[0]==='p'?'smartphone':'split','',12)}<b>${esc(g.slice(2))}</b><span>${g[0]==='p'?'Page':'Lane'} · ${ns.length}</span></div></div>`;
    }).join('');
  }
  function nodeHtml(sop,n,p,st,run){
    const ty=TYPES[n.type]||TYPES.action; const rs=run&&run[n.id];
    const isSel=st.sel===n.id||st.multi.has(n.id);
    const cls=`fx-node k-${n.type} ${PILL(n.type)?'pill':''} ${isSel?'sel':''} ${n.type==='end'&&n.outcome==='rejected'?'rej':''} ${rs?'run-'+rs:''}`;
    const ports=outPorts(n,sop).map(pt=>`<button class="fx-port out" data-port="${pt.i}" style="left:${pt.x}px" aria-label="Connect from ${esc(n.label||ty.label)}">${pt.label?`<span>${esc(pt.label)}</span>`:''}</button>`).join('');
    const runTag=rs?UI.tag(rs==='done'?'Done':rs==='blocked'?'Blocked':rs==='cur'?'Now':'Pending',rs==='done'?'ok':rs==='blocked'?'dng':rs==='cur'?'info':'mut'):'';
    const chips=PILL(n.type)?'':badges(n,sop);
    return `<div class="${cls}" data-node="${n.id}" style="transform:translate(${p.x}px,${p.y}px)">
      ${n.type!=='start'?'<i class="fx-port in"></i>':''}
      <div class="fx-eyebrow">${ic(ty.icon,'',13)}<span>${ty.label}</span>${n.page?`<em>${esc(n.page)}</em>`:''}${runTag}</div>
      <div class="fx-title">${esc(childLabel(n)||'Untitled')}</div>
      ${chips?`<div class="fx-chips">${chips}</div>`:''}
      ${ports}</div>`;
  }

  F.canvasHtml=function(sop,sel,run){
    const pos=F.positions(sop); const v=view(sop);
    if(F.zoom[sop.id]!=null&&F.zoom[sop.id]!==v.legacyZ){v.legacyZ=F.zoom[sop.id];v.k=F.zoom[sop.id];}
    const st={sel,multi:multiSet(sop),edge:F._edgeSel&&F._edgeSel.sop===sop.id?F._edgeSel.key:null};
    F._run=F._run||{}; F._run[sop.id]=run;
    schedule(sop.id);
    return `<div class="fx-root" data-flow="${sop.id}" tabindex="0" style="${bgStyle(v)}">
      <div class="fx-world" style="transform:translate(${v.x}px,${v.y}px) scale(${v.k})">
        <div class="fx-groups">${groupsHtml(sop,pos)}</div>
        <svg class="fx-edges" width="1" height="1">${edgesSvg(sop,pos,st)}</svg>
        <div class="fx-nodes">${sop.nodes.map(n=>nodeHtml(sop,n,pos[n.id],st,run)).join('')}</div>
        <svg class="fx-preview" width="1" height="1"><path d=""/></svg>
      </div>
      <div class="fx-palette" aria-label="Add step"><div class="fx-palette-h">Add step</div>
        ${PALETTE.map(t=>`<button class="fx-pal k-${t}" data-pal="${t}" title="Click to add after the selected step, or drag onto the canvas"><i class="fx-pal-ic">${ic(TYPES[t].icon,'',16)}</i><span><b>${TYPES[t].label}</b><small>${HINT[t]}</small></span></button>`).join('')}
      </div>
      <div class="fx-tools">
        <button class="fx-tb" data-fx="out" aria-label="Zoom out">${ic('zoomout','',16)}</button>
        <span class="fx-zoom">${Math.round(v.k*100)}%</span>
        <button class="fx-tb" data-fx="in" aria-label="Zoom in">${ic('zoomin','',16)}</button>
        <button class="fx-tb txt" data-fx="fit">Fit</button>
        <button class="fx-tb txt" data-fx="tidy" title="Auto-arrange every step">Tidy</button>
        <button class="fx-tb ${F.miniOff?'':'on'}" data-fx="mini" aria-label="Toggle minimap">${ic('layers','',16)}</button>
      </div>
      <div class="fx-mini ${F.miniOff?'hidden':''}"><svg></svg></div>
      <div class="fx-hint">Drag or scroll to pan · Pinch or ⌘-scroll to zoom · Drag the dot under a step to connect · Shift-click to multi-select · Del to delete · ⌘D duplicate</div>
    </div>`;
  };
  function multiSet(sop){return F.multi[sop.id]||(F.multi[sop.id]=new Set());}
  function bgStyle(v){const s=GRID*v.k;return `background-size:${s}px ${s}px;background-position:${v.x}px ${v.y}px`;}

  /* post-render: measure heights, redraw wires, first fit, minimap */
  const pending=new Set();
  function schedule(id){pending.add(id);const run=()=>{if(!pending.size)return;pending.forEach(sid=>mount(sid));pending.clear();};requestAnimationFrame(run);setTimeout(run,60);}
  function rootOf(id){return document.querySelector(`.fx-root[data-flow="${id}"]`);}
  function sopOf(id){return S.get('sops',id);}
  function mount(id){
    const root=rootOf(id),sop=sopOf(id); if(!root||!sop)return;
    measure(root,sop);
    const v=view(sop); if(v.fresh){v.fresh=false;fit(root,sop,true);}
    redraw(root,sop);
  }
  function measure(root,sop){const h=F._h[sop.id]=F._h[sop.id]||{};root.querySelectorAll('.fx-node').forEach(el=>{h[el.dataset.node]=el.offsetHeight;});}
  function redraw(root,sop,pos){
    pos=pos||F.positions(sop);
    const st={sel:(window.Work&&Work.sel[sop.id])||null,multi:multiSet(sop),edge:F._edgeSel&&F._edgeSel.sop===sop.id?F._edgeSel.key:null};
    root.querySelector('.fx-edges').innerHTML=edgesSvg(sop,pos,st);
    root.querySelector('.fx-groups').innerHTML=groupsHtml(sop,pos);
    minimap(root,sop,pos);
  }
  function applyView(root,sop){
    const v=view(sop);
    root.querySelector('.fx-world').style.transform=`translate(${v.x}px,${v.y}px) scale(${v.k})`;
    root.style.backgroundSize=`${GRID*v.k}px ${GRID*v.k}px`; root.style.backgroundPosition=`${v.x}px ${v.y}px`;
    const z=root.querySelector('.fx-zoom'); if(z)z.textContent=Math.round(v.k*100)+'%';
    minimap(root,sop);
  }
  function fit(root,sop,initial){
    const pos=F.positions(sop); const b=bounds(pos,F.byId(sop.nodes),F._h[sop.id]); const v=view(sop);
    const r=root.getBoundingClientRect(); const padL=root.querySelector('.fx-palette').offsetWidth+40, pad=48;
    const aw=Math.max(200,r.width-padL-pad), ah=Math.max(200,r.height-pad*2-20);
    let k=Math.min(aw/(b.x1-b.x0),ah/(b.y1-b.y0),1.1); k=Math.max(initial?.85:.2,k);
    v.k=k; v.x=padL+(aw-(b.x1-b.x0)*k)/2-b.x0*k; v.y=pad+Math.max(0,(ah-(b.y1-b.y0)*k)/2)-b.y0*k;
    if((b.y1-b.y0)*k>ah)v.y=pad-b.y0*k;
    applyView(root,sop);
  }
  function zoomAt(root,sop,k,cx,cy){
    const v=view(sop); k=Math.max(.2,Math.min(2.5,k));
    const wx=(cx-v.x)/v.k, wy=(cy-v.y)/v.k; v.k=k; v.x=cx-wx*k; v.y=cy-wy*k; applyView(root,sop);
  }
  function minimap(root,sop,pos){
    const mm=root.querySelector('.fx-mini svg'); if(!mm||F.miniOff)return;
    pos=pos||F.positions(sop); const m=F.byId(sop.nodes); const b=bounds(pos,m,F._h[sop.id]); const v=view(sop);
    const r=root.getBoundingClientRect(); const vx0=-v.x/v.k,vy0=-v.y/v.k,vx1=vx0+r.width/v.k,vy1=vy0+r.height/v.k;
    const x0=Math.min(b.x0,vx0)-40,y0=Math.min(b.y0,vy0)-40,x1=Math.max(b.x1,vx1)+40,y1=Math.max(b.y1,vy1)+40;
    const W=180,H=120,s=Math.min(W/(x1-x0),H/(y1-y0));
    root._mini={x0,y0,s};
    mm.setAttribute('viewBox',`0 0 ${W} ${H}`);
    mm.innerHTML=sop.nodes.map(n=>{const p=pos[n.id];if(!p)return '';return `<rect class="k-${n.type}" x="${(p.x-x0)*s}" y="${(p.y-y0)*s}" width="${NW*s}" height="${Math.max(2,hOf(sop,n)*s)}" rx="1.5"/>`;}).join('')
      +`<rect class="vp" x="${(vx0-x0)*s}" y="${(vy0-y0)*s}" width="${(vx1-vx0)*s}" height="${(vy1-vy0)*s}"/>`;
  }

  /* ---------- persistence ---------- */
  function persist(sop,pos){sop.layout={};sop.nodes.forEach(n=>{if(pos[n.id])sop.layout[n.id]={x:pos[n.id].x,y:pos[n.id].y};});S.save();}
  function select(sop,id,additive){
    const ms=multiSet(sop);
    if(additive){if(!id)return;const cur=Work.sel[sop.id];if(cur&&cur!==id)ms.add(cur);if(ms.has(id)){ms.delete(id);if(Work.sel[sop.id]===id)Work.sel[sop.id]=[...ms].pop()||null;}else{ms.add(id);Work.sel[sop.id]=id;}if(ms.size===1&&ms.has(Work.sel[sop.id]))ms.clear();}
    else{ms.clear();Work.sel[sop.id]=id||null;}
    F._edgeSel=null; App.render();
  }
  function selectedIds(sop){const ms=multiSet(sop);const ids=new Set(ms);const s=Work.sel[sop.id];if(s)ids.add(s);return [...ids].filter(id=>sop.nodes.some(n=>n.id===id));}
  function refocus(id){requestAnimationFrame(()=>{const r=rootOf(id);if(r)r.focus({preventScroll:true});});}

  /* structural helpers used by interactions */
  function connect(sop,from,port,to){
    const n=sop.nodes.find(x=>x.id===from); if(!n||from===to||n.type==='end')return false;
    const t=sop.nodes.find(x=>x.id===to); if(!t||t.type==='start')return false;
    S.snap('Connect steps');
    n.next=n.next||[];
    if(n.type==='decision'){
      if(n.next.length<2&&port>=n.next.length)n.next.push({to,when:n.next.length===0?'Yes':'No'});
      else if(n.next[port])n.next[port].to=to; else n.next.push({to,when:'Branch '+(n.next.length+1)});
    }else if(n.type==='parallel'){
      if(n.next[port])n.next[port].to=to; else n.next.push({to,when:'Lane '+String.fromCharCode(65+n.next.length)});
    }else{
      if(n.next.length)n.next[0].to=to; else n.next=[{to}];
    }
    return true;
  }
  function addAt(sop,type,x,y){
    const nodes=sop.nodes; const pos=F.positions(sop);
    S.snap('Add step');
    const n=Object.assign({id:F.nid(nodes)},F.defaults(type,sop)); nodes.push(n);
    pos[n.id]={x:snap(x-NW/2),y:snap(y-30)}; persist(sop,pos);
    return n;
  }
  function insertAfterSel(sop,type,afterId){
    const a=sop.nodes.find(x=>x.id===afterId); if(!a||a.type==='end')return null;
    const pos=F.positions(sop); persist(sop,pos);
    S.snap('Add step');
    const before=new Set(sop.nodes.map(n=>n.id));
    const n=F.insertAfter(sop,afterId,type); if(!n){S.undo();return null;}
    // push everything below the anchor down to make room, place new nodes under the anchor
    const added=sop.nodes.filter(x=>!before.has(x.id));
    const ap=pos[afterId]; const baseY=ap.y+estH(a)+RH; const need=added.length>1?(estH(n)+RH)*3:estH(n)+RH;
    Object.keys(pos).forEach(id=>{if(id!==afterId&&pos[id].y>=baseY-10&&Math.abs(pos[id].x-ap.x)<CW*3)pos[id].y+=need;});
    if(type==='parallel'){const lanes=added.filter(x=>x.lane),join=added.find(x=>x.type==='join');
      pos[n.id]={x:ap.x,y:baseY};lanes.forEach((l,i)=>pos[l.id]={x:ap.x+(i-(lanes.length-1)/2)*CW,y:snap(baseY+estH(n)+RH)});
      if(join)pos[join.id]={x:ap.x,y:snap(baseY+estH(n)+RH+estH(lanes[0]||n)+RH)};}
    else added.forEach(x=>pos[x.id]={x:ap.x,y:snap(baseY)});
    persist(sop,pos); return n;
  }
  function duplicate(sop,ids){
    const pos=F.positions(sop); S.snap('Duplicate step'); const out=[];
    ids.forEach(id=>{const n=sop.nodes.find(x=>x.id===id);if(!n||n.type==='start')return;
      const c=JSON.parse(JSON.stringify(n));c.id=F.nid(sop.nodes);sop.nodes.push(c);pos[c.id]={x:pos[id].x+40,y:pos[id].y+40};out.push(c.id);});
    persist(sop,pos); return out;
  }

  /* ---------- interactions (delegated, one set of listeners for any editor on the page) ---------- */
  let drag=null, spaceDown=false;
  const local=(root,e)=>{const r=root.getBoundingClientRect();return {x:e.clientX-r.left,y:e.clientY-r.top};};
  const world=(root,sop,e)=>{const p=local(root,e),v=view(sop);return {x:(p.x-v.x)/v.k,y:(p.y-v.y)/v.k};};

  document.addEventListener('pointerdown',e=>{
    const root=e.target.closest&&e.target.closest('.fx-root'); if(!root)return;
    const sop=sopOf(root.dataset.flow); if(!sop)return;
    if(e.target.closest('.fx-tools,.fx-hint'))return;
    if(e.button===2)return;
    root.focus({preventScroll:true});
    const mini=e.target.closest('.fx-mini');
    if(mini){e.preventDefault();drag={kind:'mini',root,sop};miniJump(root,sop,e);capture(root,e);return;}
    const pal=e.target.closest('[data-pal]');
    if(pal){e.preventDefault();drag={kind:'pal',root,sop,type:pal.dataset.pal,sx:e.clientX,sy:e.clientY,moved:false,ghost:null};capture(root,e);return;}
    if(e.target.closest('.fx-palette'))return;
    const pan=e.button===1||spaceDown;
    const port=!pan&&e.target.closest('.fx-port.out');
    const nodeEl=!pan&&e.target.closest('.fx-node');
    const groupEl=!pan&&e.target.closest('[data-group]');
    const edgeEl=!pan&&e.target.closest('[data-edge]');
    e.preventDefault();
    if(port&&nodeEl){
      const n=sop.nodes.find(x=>x.id===nodeEl.dataset.node);
      drag={kind:'wire',root,sop,from:n.id,port:+port.dataset.port,pos:F.positions(sop)};
      root.classList.add('connecting'); capture(root,e); wireMove(e); return;
    }
    if(nodeEl){
      const id=nodeEl.dataset.node; const pos=F.positions(sop); const ms=multiSet(sop);
      let ids=(ms.has(id)||Work.sel[sop.id]===id)&&ms.size?selectedIds(sop):[id];
      if(!ids.includes(id))ids=[id];
      drag={kind:'node',root,sop,id,ids,pos,start:world(root,sop,e),orig:Object.fromEntries(ids.map(i=>[i,{...pos[i]}])),moved:false,shift:e.shiftKey};
      capture(root,e); return;
    }
    if(groupEl){
      const g=groupEl.dataset.group; const pos=F.positions(sop);
      const ids=sop.nodes.filter(n=>(g[0]==='p'?n.page:n.lane)===g.slice(2)).map(n=>n.id);
      drag={kind:'node',root,sop,id:null,ids,pos,start:world(root,sop,e),orig:Object.fromEntries(ids.map(i=>[i,{...pos[i]}])),moved:false,group:true};
      capture(root,e); return;
    }
    if(edgeEl){F._edgeSel={sop:sop.id,key:edgeEl.dataset.edge};redraw(root,sop);return;}
    drag={kind:'pan',root,sop,sx:e.clientX,sy:e.clientY,vx:view(sop).x,vy:view(sop).y,moved:false};
    root.classList.add('panning'); capture(root,e);
  });
  function capture(root,e){try{root.setPointerCapture(e.pointerId);}catch(_){}}

  document.addEventListener('pointermove',e=>{
    if(!drag)return; const {root,sop}=drag;
    if(drag.kind==='pan'){const v=view(sop);if(Math.abs(e.clientX-drag.sx)+Math.abs(e.clientY-drag.sy)>3)drag.moved=true;v.x=drag.vx+e.clientX-drag.sx;v.y=drag.vy+e.clientY-drag.sy;applyView(root,sop);return;}
    if(drag.kind==='mini'){miniJump(root,sop,e);return;}
    if(drag.kind==='wire'){wireMove(e);return;}
    if(drag.kind==='pal'){
      if(!drag.moved&&Math.abs(e.clientX-drag.sx)+Math.abs(e.clientY-drag.sy)>5){drag.moved=true;
        const g=document.createElement('div');g.className='fx-ghost k-'+drag.type;g.innerHTML=`${ic(TYPES[drag.type].icon,'',14)}<b>${TYPES[drag.type].label}</b>`;document.body.appendChild(g);drag.ghost=g;}
      if(drag.ghost){drag.ghost.style.transform=`translate(${e.clientX+8}px,${e.clientY+8}px)`;
        root.querySelectorAll('.fx-node.drop').forEach(x=>x.classList.remove('drop'));
        const over=document.elementFromPoint(e.clientX,e.clientY); const t=over&&over.closest('.fx-node'); if(t&&root.contains(t))t.classList.add('drop');}
      return;
    }
    if(drag.kind==='node'){
      const w=world(root,sop,e); const dx=w.x-drag.start.x, dy=w.y-drag.start.y;
      if(!drag.moved&&Math.hypot(dx,dy)*view(sop).k<4)return;
      if(!drag.moved){drag.moved=true;S.snap('Move step');root.classList.add('dragging');}
      drag.ids.forEach(id=>{const o=drag.orig[id];const x=e.altKey?o.x+dx:snap(o.x+dx),y=e.altKey?o.y+dy:snap(o.y+dy);drag.pos[id]={x,y};
        const el=root.querySelector(`.fx-node[data-node="${id}"]`);if(el){el.style.transform=`translate(${x}px,${y}px)`;el.classList.add('lift');}});
      redraw(root,sop,drag.pos);
    }
  });
  function wireMove(e){
    const {root,sop}=drag; const w=world(root,sop,e); const n=sop.nodes.find(x=>x.id===drag.from);
    const pt=outPorts(n,sop).find(p=>p.i===drag.port)||{x:NW/2}; const p=drag.pos[n.id];
    const sx=p.x+pt.x, sy=p.y+hOf(sop,n);
    root.querySelector('.fx-preview path').setAttribute('d',`M${sx},${sy} C${sx},${sy+60} ${w.x},${w.y-60} ${w.x},${w.y}`);
    root.querySelectorAll('.fx-node.drop').forEach(x=>x.classList.remove('drop'));
    const over=document.elementFromPoint(e.clientX,e.clientY); const t=over&&over.closest('.fx-node');
    if(t&&root.contains(t)&&t.dataset.node!==drag.from)t.classList.add('drop');
  }
  function miniJump(root,sop,e){
    const mm=root.querySelector('.fx-mini svg'); const info=root._mini; if(!mm||!info)return;
    const r=mm.getBoundingClientRect(); const sc=180/r.width;
    const wx=(e.clientX-r.left)*sc/info.s+info.x0, wy=(e.clientY-r.top)*sc/info.s+info.y0;
    const rr=root.getBoundingClientRect(); const v=view(sop); v.x=rr.width/2-wx*v.k; v.y=rr.height/2-wy*v.k; applyView(root,sop);
  }

  document.addEventListener('pointerup',e=>{
    if(!drag)return; const d=drag; drag=null; const {root,sop}=d;
    root.classList.remove('panning','dragging','connecting');
    root.querySelectorAll('.fx-node.drop').forEach(x=>x.classList.remove('drop'));
    if(d.kind==='pan'){if(!d.moved&&(Work.sel[sop.id]||multiSet(sop).size||F._edgeSel)){F._edgeSel=null;select(sop,null);}return;}
    if(d.kind==='wire'){
      root.querySelector('.fx-preview path').setAttribute('d','');
      const over=document.elementFromPoint(e.clientX,e.clientY); const t=over&&over.closest('.fx-node');
      if(t&&root.contains(t)&&connect(sop,d.from,d.port,t.dataset.node)){S.save();App.render();refocus(sop.id);}
      return;
    }
    if(d.kind==='pal'){
      if(d.ghost)d.ghost.remove();
      if(!d.moved){
        const after=Work.sel[sop.id]; const a=sop.nodes.find(x=>x.id===after);
        let n;
        if(a&&a.type!=='end')n=insertAfterSel(sop,d.type,after);
        else{const r=root.getBoundingClientRect(),v=view(sop);n=addAt(sop,d.type,(r.width/2-v.x)/v.k,(r.height/2-v.y)/v.k);UI.toast('Added — drag its dot to connect it');}
        if(n){S.save();multiSet(sop).clear();Work.sel[sop.id]=n.id;App.render();refocus(sop.id);}
        return;
      }
      const rr=root.getBoundingClientRect(); if(e.clientX<rr.left||e.clientX>rr.right||e.clientY<rr.top||e.clientY>rr.bottom)return;
      if(document.elementFromPoint(e.clientX,e.clientY)?.closest('.fx-palette'))return;
      const over=document.elementFromPoint(e.clientX,e.clientY); const t=over&&over.closest('.fx-node');
      let n;
      if(t&&root.contains(t))n=insertAfterSel(sop,d.type,t.dataset.node);
      if(!n){const w=world(root,sop,e);n=addAt(sop,d.type,w.x,w.y);}
      S.save();multiSet(sop).clear();Work.sel[sop.id]=n.id;App.render();refocus(sop.id);return;
    }
    if(d.kind==='node'){
      if(d.moved){persist(sop,d.pos);measure(root,sop);redraw(root,sop,d.pos);root.querySelectorAll('.fx-node.lift').forEach(x=>x.classList.remove('lift'));
        if(App.renderCanvasOnly){App.renderCanvasOnly();refocus(sop.id);}return;}
      if(d.group)return;
      if(d.shift){select(sop,d.id,true);refocus(sop.id);return;}
      if(Work.sel[sop.id]!==d.id||multiSet(sop).size){select(sop,d.id,false);refocus(sop.id);}
    }
  });
  document.addEventListener('pointercancel',()=>{if(drag){if(drag.ghost)drag.ghost.remove();drag.root.classList.remove('panning','dragging','connecting');drag=null;}});

  document.addEventListener('wheel',e=>{
    const root=e.target.closest&&e.target.closest('.fx-root'); if(!root||e.target.closest('.fx-palette'))return;
    const sop=sopOf(root.dataset.flow); if(!sop)return; e.preventDefault();
    const v=view(sop);
    const m=e.deltaMode===1?16:1; const dx=e.deltaX*m, dy=e.deltaY*m;
    if(e.ctrlKey||e.metaKey){const p=local(root,e);zoomAt(root,sop,v.k*Math.exp(-dy*(e.ctrlKey?0.01:0.0022)),p.x,p.y);return;}
    if(e.shiftKey&&!dx){v.x-=dy;}else{v.x-=dx;v.y-=dy;}
    applyView(root,sop);
  },{passive:false});

  document.addEventListener('click',e=>{
    const b=e.target.closest&&e.target.closest('.fx-root [data-fx]'); if(!b)return;
    const root=b.closest('.fx-root'),sop=sopOf(root.dataset.flow); if(!sop)return;
    const r=root.getBoundingClientRect(),v=view(sop),fx=b.dataset.fx;
    if(fx==='in')zoomAt(root,sop,v.k*1.2,r.width/2,r.height/2);
    if(fx==='out')zoomAt(root,sop,v.k/1.2,r.width/2,r.height/2);
    if(fx==='fit')fit(root,sop);
    if(fx==='tidy'){S.snap('Tidy layout');delete sop.layout;S.save();view(sop).fresh=true;App.render();UI.toast('Steps re-arranged',()=>{S.undo();App.render();});}
    if(fx==='mini'){F.miniOff=!F.miniOff;root.querySelector('.fx-mini').classList.toggle('hidden',F.miniOff);b.classList.toggle('on',!F.miniOff);minimap(root,sop);}
  });

  const typing=t=>t&&(t.isContentEditable||/^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName));
  document.addEventListener('keydown',e=>{
    if(e.code==='Space'&&!typing(e.target)&&document.querySelector('.fx-root')){if(!spaceDown){spaceDown=true;document.querySelectorAll('.fx-root').forEach(r=>r.classList.add('space'));}if(e.target.closest&&e.target.closest('.fx-root'))e.preventDefault();return;}
    const root=e.target.closest&&e.target.closest('.fx-root'); if(!root)return;
    const sop=sopOf(root.dataset.flow); if(!sop)return;
    const ids=selectedIds(sop);
    if(e.key==='Delete'||e.key==='Backspace'){
      e.preventDefault();
      if(F._edgeSel&&F._edgeSel.sop===sop.id){const [nid,i]=F._edgeSel.key.split(':');const n=sop.nodes.find(x=>x.id===nid);
        if(n){S.snap('Delete connection');n.next.splice(+i,1);S.save();F._edgeSel=null;App.render();refocus(sop.id);UI.toast('Connection removed',()=>{S.undo();App.render();});}return;}
      if(!ids.length)return;
      S.snap('Delete step'); let removed=0;
      ids.forEach(id=>{if(F.remove(sop,id))removed++;});
      if(!removed){S.undo();UI.toast('This step cannot be deleted');return;}
      if(sop.layout)ids.forEach(id=>delete sop.layout[id]);
      S.save(); multiSet(sop).clear(); Work.sel[sop.id]=null; App.render(); refocus(sop.id);
      UI.toast(removed>1?removed+' steps deleted':'Step deleted',()=>{S.undo();App.render();});
    }else if((e.metaKey||e.ctrlKey)&&(e.key==='d'||e.key==='D')){
      e.preventDefault(); if(!ids.length)return;
      const out=duplicate(sop,ids); if(!out.length)return; S.save();
      const ms=multiSet(sop);ms.clear();if(out.length>1)out.forEach(i=>ms.add(i));Work.sel[sop.id]=out[out.length-1];App.render();refocus(sop.id);
    }else if(e.key==='Escape'){multiSet(sop).clear();F._edgeSel=null;if(Work.sel[sop.id]){Work.sel[sop.id]=null;App.render();refocus(sop.id);}}
    else if((e.metaKey||e.ctrlKey)&&e.key==='0'){e.preventDefault();fit(root,sop);}
    else if((e.key==='='||e.key==='+')&&!e.metaKey&&!e.ctrlKey){const r=root.getBoundingClientRect();zoomAt(root,sop,view(sop).k*1.2,r.width/2,r.height/2);}
    else if(e.key==='-'&&!e.metaKey&&!e.ctrlKey){const r=root.getBoundingClientRect();zoomAt(root,sop,view(sop).k/1.2,r.width/2,r.height/2);}
  });
  document.addEventListener('keyup',e=>{if(e.code==='Space'){spaceDown=false;document.querySelectorAll('.fx-root').forEach(r=>r.classList.remove('space'));}});
  window.addEventListener('resize',()=>{document.querySelectorAll('.fx-root').forEach(r=>{const s=sopOf(r.dataset.flow);if(s)minimap(r,s);});});

  /* ---------- structural edits ---------- */
  F.defaults=function(type,sop){
    const base={type,label:'',next:[]};
    if(type==='question')Object.assign(base,{label:'New question',answer:'choice',required:true,options:[{v:'yes',l:'Yes'},{v:'no',l:'No'}]});
    if(type==='evidence')Object.assign(base,{label:'Photo proof',media:['photo'],min:1,max:1});
    if(type==='decision')Object.assign(base,{label:'Check answer',q:'',op:'=',value:''});
    if(type==='approval')Object.assign(base,{label:'Approval',roleId:'role_verifier',outcomes:['Approve','Send back']});
    if(type==='wait')Object.assign(base,{label:'Wait',amount:1,unit:'days'});
    if(type==='repeat')Object.assign(base,{label:'Repeat check',every:3,everyUnit:'hours',forAmount:1,forUnit:'days',media:['photo']});
    if(type==='child')Object.assign(base,{label:'Follow SOP',sopId:''});
    if(type==='action')Object.assign(base,{label:'New task'});
    if(type==='end')Object.assign(base,{label:'End',outcome:'done'});
    if(type==='parallel')Object.assign(base,{label:'In parallel'});
    return base;
  };
  F.insertAfter=function(sop,afterId,type){
    const nodes=sop.nodes, a=nodes.find(n=>n.id===afterId); if(!a||a.type==='end')return null;
    const oldTo=(a.next[0]||{}).to;
    const mk=t=>{const n=Object.assign({id:F.nid(nodes)},F.defaults(t,sop));if(a.page&&['question','evidence','action','decision'].includes(t))n.page=a.page;nodes.push(n);return n;};
    const n=mk(type);
    if(type==='decision'){
      const q=[...nodes].reverse().find(x=>x.type==='question'&&x.id!==n.id);
      if(q){n.q=q.id;n.value=(q.options&&q.options[0]||{}).v||'';n.label=q.label+'?';}
      n.next=oldTo?[{to:oldTo,when:'Yes'},{to:oldTo,when:'No'}]:[];
    }else if(type==='parallel'){
      const l1=mk('action'),l2=mk('action'),j=mk('join');
      l1.label='Lane A task';l1.lane='Lane A';l2.label='Lane B task';l2.lane='Lane B';j.label='Both lanes done';
      delete l1.page;delete l2.page;
      n.next=[{to:l1.id,when:'Lane A'},{to:l2.id,when:'Lane B'}];l1.next=[{to:j.id}];l2.next=[{to:j.id}];j.next=oldTo?[{to:oldTo}]:[];
    }else if(type==='end'){n.next=[];}
    else n.next=oldTo?[{to:oldTo}]:[];
    if(a.next.length)a.next[0].to=n.id; else a.next=[{to:n.id}];
    return n;
  };
  F.remove=function(sop,id){
    const nodes=sop.nodes,n=nodes.find(x=>x.id===id); if(!n||n.type==='start')return false;
    if(n.type==='end'&&nodes.filter(x=>x.type==='end').length<2)return false;
    const to=(n.next[0]||{}).to;
    nodes.forEach(x=>x.next=(x.next||[]).map(e=>e.to===id?(to?Object.assign({},e,{to}):null):e).filter(Boolean));
    nodes.forEach(x=>{if(x.onlyIf&&x.onlyIf.q===id)delete x.onlyIf;if(x.type==='decision'&&x.q===id)x.q='';});
    sop.nodes=nodes.filter(x=>x.id!==id);
    return true;
  };
  window.Flow=F;
})();
