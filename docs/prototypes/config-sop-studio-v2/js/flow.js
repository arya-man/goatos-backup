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
  const REFS={pens:'Pens',partitions:'Partitions',vendors:'Vendors',parks:'Parks',items:'Items',people:'People',movementReasons:'Movement reasons'};
  F.REFS=REFS;

  F.byId=nodes=>{const m={};nodes.forEach(n=>m[n.id]=n);return m;};
  F.stepCount=nodes=>nodes.filter(n=>!['start','end','join','parallel'].includes(n.type)).length;
  F.pages=nodes=>[...new Set(nodes.map(n=>n.page).filter(Boolean))];
  F.nid=nodes=>{let i=nodes.length+1;const ids=new Set(nodes.map(n=>n.id));while(ids.has('n'+i))i++;return 'n'+i;};

  /* ---------- geometry constants ---------- */
  const NW=264, GRID=20, RH=72, CW=320;
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
    let v=n.value; const vs=n.valueSetting&&S.get('settings',n.valueSetting); if(vs)v=vs.value; else if(typeof v==='string'&&v.startsWith('@'))v=answers[v.slice(1)];
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
    if(!F.stepCount(nodes))iss.push({id:(start||{}).id,msg:'Add at least one step'});
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
      if(n.type==='approval'&&!(n.chainId?S.get('approvalChains',n.chainId)&&F.approvers(n).length:(n.roleId&&S.get('roles',n.roleId))))iss.push({id:n.id,msg:L+': choose an approval chain or role'});
      if(n.type==='child'&&!S.get('sops',n.sopId))iss.push({id:n.id,msg:L+': choose child SOP'});
      if(n.type==='child'&&n.sopId===sop.id)iss.push({id:n.id,msg:L+': cannot follow itself'});
      else if(n.type==='child'&&n.sopId&&F.childLoop(n.sopId,sop.id))iss.push({id:n.id,msg:L+': loops back to this SOP'});
    });
    if(!nodes.some(n=>n.type==='end'))iss.push({msg:'No end step'});
    return iss;
  };
  /* true when SOP `fromId` (via child steps, any depth) reaches `targetId`; target omitted = any cycle below fromId */
  F.childLoop=function(fromId,targetId){
    const seen=new Set(),stack=new Set();
    const walk=id=>{if(id===targetId)return true;if(stack.has(id))return !targetId;if(seen.has(id))return false;seen.add(id);stack.add(id);
      const s=S.get('sops',id);const hit=!!s&&(s.nodes||[]).some(n=>n.type==='child'&&n.sopId&&walk(n.sopId));stack.delete(id);return hit;};
    const s=S.get('sops',fromId); if(!s)return false; seen.add(fromId); stack.add(fromId);
    return (s.nodes||[]).some(n=>n.type==='child'&&n.sopId&&(n.sopId===fromId?!targetId:walk(n.sopId)));
  };
  F.META=['title','dept','category'];
  /* key-order-insensitive compare; undefined keys dropped (an inspector delete+re-add must not read as an edit) */
  F.stable=function(o){return JSON.stringify(o,function(k,v){if(v&&typeof v==='object'&&!Array.isArray(v)){const r={};Object.keys(v).sort().forEach(x=>{if(v[x]!==undefined)r[x]=v[x];});return r;}return v;});};
  /* ---------- settings referenced by steps: pinned per published version ---------- */
  let seedSettings=null;
  const seedSetting=id=>{if(!seedSettings){try{seedSettings={};((window.buildSeed&&window.buildSeed().settings)||[]).forEach(x=>seedSettings[x.id]=x.value);}catch(e){seedSettings={};}}return seedSettings[id];};
  F.settingIds=function(obj){const out=[];const walk=(v,k)=>{if(k==='next'||k==='label'||v==null)return;
      if(typeof v==='string'){if(/^set_/.test(v)&&S.get('settings',v)&&!out.includes(v))out.push(v);return;}
      if(Array.isArray(v))v.forEach(x=>walk(x));else if(typeof v==='object')Object.keys(v).forEach(x=>walk(v[x],x));};
    walk(obj);return out;};
  /* {settingId: value} the version was published with; versions from before pinning take the seeded value */
  F.pinsOf=function(ver,items){if(!ver)return {};
    const refs=F.settingIds(items||ver.nodes||ver.stages||[]); ver.pins=Array.isArray(ver.pins)?ver.pins:[];
    refs.forEach(id=>{if(ver.pins.some(p=>p.id===id))return;const st=S.get('settings',id);const sv=seedSetting(id);
      ver.pins.push({id,name:st?st.name:id,value:sv!==undefined?sv:(st?st.value:''),unit:st?st.unit:''});});
    const m={};ver.pins.forEach(p=>m[p.id]=p.value);return m;};
  F.pinNow=function(items){return F.settingIds(items).map(id=>{const st=S.get('settings',id);return {id,name:st.name,value:st.value,unit:st.unit};});};
  /* settings whose value moved since the published version: [{id,name,unit,was,now}] */
  F.drift=function(items,ver){if(!ver)return [];const pins=F.pinsOf(ver,ver.nodes||ver.stages);
    return F.settingIds(items).map(id=>{const st=S.get('settings',id);return st&&pins[id]!==undefined&&String(pins[id])!==String(st.value)?{id,name:st.name,unit:st.unit,was:pins[id],now:st.value}:null;}).filter(Boolean);};
  F.settingChips=function(obj,ver){const d={};F.drift(obj,ver).forEach(x=>d[x.id]=x);
    return F.settingIds(obj).map(id=>{const st=S.get('settings',id);const x=d[id];
      return `<div class="fx-set ${x?'moved':''}" title="${x?'Running work keeps '+esc(x.was)+'; publish to use '+esc(x.now):'From business settings'}">Uses setting: ${esc(st.name)} = ${esc(st.value)}${st.unit&&st.unit!=='time'?' '+esc(st.unit):''}${x?` <b>(was ${esc(x.was)})</b>`:''}</div>`;}).join('');};
  F.changed=function(sop){const v=sop.versions[sop.versions.length-1];if(!v)return true;
    F.META.forEach(k=>{if(v[k]===undefined)v[k]=sop[k];});
    F.linkChains(sop);
    return F.stable(v.nodes)!==F.stable(sop.nodes)||F.META.some(k=>v[k]!==sop[k])||F.drift(sop.nodes,v).length>0;};
  /* approvers of an approval step: its chain (live) or its single role */
  F.approvers=function(n){
    if(n.chainId){const ch=S.get('approvalChains',n.chainId);if(!ch)return [];
      const list=(window.REG&&typeof REG.approversFor==='function')?(REG.approversFor(ch.id)||[]):(ch.steps||[]).map(x=>({label:x.role||x.designation||x.person}));
      return list.map(x=>typeof x==='string'?{label:x}:x).filter(x=>x&&x.label);}
    return n.roleId&&S.get('roles',n.roleId)?[{label:REG.labelById('roles',n.roleId)}]:[];};
  /* seeded approval steps whose role is the first approver of their department's chain follow that chain */
  F.linkChains=function(sop){
    const chains=S.active('approvalChains'); if(!chains.length)return;
    const t=REG.norm(sop.title); const ch=chains.find(c=>c.dept===sop.dept&&(t===REG.norm(c.name)||t.startsWith(REG.norm(c.name)+' ')));
    if(!ch)return; const keys=(ch.steps||[]).map(x=>x.role||x.designation).filter(Boolean);
    const vn={};(F.latest(sop)?F.latest(sop).nodes:[]).forEach(x=>vn[x.id]=x);
    sop.nodes.forEach(n=>{if(n.type!=='approval'||n.chainId!==undefined)return;const r=S.get('roles',n.roleId);if(!r||!keys.includes(r.key))return;
      n.chainId=ch.id;if(vn[n.id]&&vn[n.id].chainId===undefined&&vn[n.id].roleId===n.roleId)vn[n.id].chainId=ch.id;});
  };
  /* step-level diff vs a version: [{kind:'added'|'removed'|'changed', label}] plus SOP detail changes */
  F.diff=function(sop,v){if(!v)return sop.nodes.filter(n=>!['start','end'].includes(n.type)).map(n=>({kind:'added',label:n.label||F.TYPES[n.type].label,type:n.type}));
    const out=[];const old=F.byId(v.nodes),cur=F.byId(sop.nodes);
    F.META.forEach(k=>{if(v[k]!==undefined&&v[k]!==sop[k])out.push({kind:'changed',label:(k==='dept'?'Department':k[0].toUpperCase()+k.slice(1))+': '+v[k]+' → '+sop[k],type:'meta'});});
    sop.nodes.forEach(n=>{const o=old[n.id];if(!o)out.push({kind:'added',label:n.label||F.TYPES[n.type].label,type:n.type});else if(F.stable(o)!==F.stable(n))out.push({kind:'changed',label:n.label||F.TYPES[n.type].label,type:n.type});});
    v.nodes.forEach(o=>{if(!cur[o.id])out.push({kind:'removed',label:o.label||F.TYPES[o.type].label,type:o.type});});
    F.drift(sop.nodes,v).forEach(d=>out.push({kind:'changed',label:'Setting '+d.name+': '+d.was+' → '+d.now+(d.unit&&d.unit!=='time'?' '+d.unit:'')+' (running work keeps '+d.was+')',type:'setting'}));
    return out;};
  F.latest=sop=>sop.versions[sop.versions.length-1];

  /* one plain line under the title */
  F.summary=function(n,sop){
    const m=F.byId(sop.nodes); const setv=(id,v,u)=>{const s=S.get('settings',id);return (s?s.value:v)+' '+(s?s.unit:u||'');};
    switch(n.type){
      case 'start':return 'Operator starts';
      case 'question':{const a=ANSWERS[n.answer]||'Text';const o=(n.options||[]).map(o=>o.l);
        const lim=n.answer==='number'?F.limitText(n,sop):'';
        return [a==='Single choice'||a==='Multiple choice'?(o.slice(0,3).join(' / ')+(o.length>3?' …':'')||a):a,n.unit,lim,n.required?'required':'',n.reject?'can reject':''].filter(Boolean).join(' · ');}
      case 'evidence':return (n.media||['photo']).join(' or ')+' · '+(n.min||0)+(n.max&&n.max!==n.min?'–'+n.max:'')+' file'+((n.max||1)>1?'s':'');
      case 'decision':{const q=m[n.q];if(n.op==='duplicate')return 'Already scanned?';if(!q)return 'Choose a question';
        const val=String(n.value).startsWith('@')?(m[n.value.slice(1)]||{}).label:(((q.options||[]).find(o=>o.v===n.value)||{}).l||n.value);return 'If answer '+n.op+' '+val;}
      case 'approval':{const ap=F.approvers(n);if(n.chainId){const ch=S.get('approvalChains',n.chainId);return ch&&ap.length?ch.name+' chain: '+ap.map(x=>x.label).join(' → '):'Choose an approval chain';}
        return ap.length?ap[0].label+' signs off':'Choose approver';}
      case 'wait':return n.until==='age'?'Until '+n.amount+' days old':'Wait '+setv(n.setting,n.amount,n.unit);
      case 'repeat':if((n.times||[]).length)return n.times.length+'×/day ('+n.times[0]+'–'+n.times[n.times.length-1]+') · '+(n.slotCount||n.times.length*(n.forAmount||1))+' checks';
        return 'Every '+setv(n.everySetting,n.every,n.everyUnit)+' for '+setv(n.forSetting,n.forAmount,n.forUnit||'days');
      case 'child':{const c=S.get('sops',n.sopId);return c?'Runs '+F.stepCount(c.nodes)+' steps':'Choose SOP';}
      case 'action':{if(n.link)return 'Opens '+(n.linkLabel||'linked module');const it=S.get('items',n.itemId);return it?it.name+(n.dose?' · '+n.dose+(it.unit?' '+it.unit:''):''):'Operator ticks when done';}
      case 'parallel':return (n.next||[]).length+' lanes at once';
      case 'join':return 'All lanes done';
      case 'end':return n.outcome==='rejected'?'Rejected':'Completed';
    }
    return '';
  };
  /* number limits: fixed min/max, or read from a setting (optionally per answer of a choice question) */
  F.limitSetting=function(spec,sop,answers){if(!spec)return null;
    if(typeof spec==='string')return S.get('settings',spec)||null;
    const keys=Object.keys(spec); if(!keys.length)return null;
    const q=(sop.nodes||[]).find(x=>x.type==='question'&&(x.options||[]).some(o=>keys.includes(o.v)));
    const a=q&&answers?answers[q.id]:undefined; const k=q?(a!==undefined&&spec[a]?a:null):(keys.length===1?keys[0]:null);
    return k?S.get('settings',spec[k])||null:null;};
  F.limits=function(n,sop,answers){const rule=n.rule||{};
    const mx=F.limitSetting(rule.maxFromSetting||n.maxFromSetting,sop,answers),mn=F.limitSetting(rule.minFromSetting||n.minFromSetting,sop,answers);
    return {min:mn?Number(mn.value):(n.min==null||n.min===''?null:Number(n.min)),max:mx?Number(mx.value):(n.max==null||n.max===''?null:Number(n.max)),minSet:mn,maxSet:mx};};
  F.limitText=function(n,sop){const rule=n.rule||{};const has=rule.maxFromSetting||n.maxFromSetting||rule.minFromSetting||n.minFromSetting;
    if(has)return 'limits from settings';const L=F.limits(n,sop);return L.min!=null&&L.max!=null?L.min+'–'+L.max:L.max!=null?'max '+L.max:'';};
  function childLabel(n){if(n.type!=='child')return n.label;const c=S.get('sops',n.sopId);return c?c.title:n.label;}

  /* ---------- stylesheet (owned by this module) ---------- */
  (function(){if(document.querySelector('link[data-flow-css]'))return;const l=document.createElement('link');l.rel='stylesheet';l.href='styles/flow.css';l.dataset.flowCss='1';l.onload=()=>document.querySelectorAll('.fx-root').forEach(r=>syncChrome(r));document.head.appendChild(l);})();

  /* ---------- rendering ---------- */
  const PALETTE=['question','evidence','decision','approval','wait','repeat','child','action','parallel','end'];
  F._h={};
  const hOf=(sop,n)=>((F._h[sop.id]||{})[n.id])||estH(n);

  function view(sop){return F.view[sop.id]||(F.view[sop.id]={x:60,y:40,k:1,fresh:true,auto:true});}
  F.miniOff=true; F._focus={};

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
      if(ty-my>=40)return {d,lx,ly,px:tx,py:(my+ty)/2};
      return {d,lx,ly,px:(sx+tx)/2,py:my};
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
  function plusHtml(sop,pos){
    let out='';
    sop.nodes.forEach(n=>(n.next||[]).forEach((e,i)=>{const g=edgeGeom(sop,pos,n,e,i);if(!g||g.px==null)return;
      out+=`<button class="fx-plus ${F._edgeSel&&F._edgeSel.sop===sop.id&&F._edgeSel.key===n.id+':'+i?'show':''}" data-plus="${n.id}:${i}" style="left:${g.px}px;top:${g.py}px" aria-label="Insert step here">+</button>`;}));
    return out;
  }
  function groupsHtml(sop,pos){
    const groups={};
    sop.nodes.forEach(n=>{const g=n.page?'p:'+n.page:(n.lane?'l:'+n.lane:null);if(!g||!pos[n.id])return;(groups[g]=groups[g]||[]).push(n);});
    return Object.entries(groups).map(([g,ns])=>{
      const x0=Math.min(...ns.map(n=>pos[n.id].x))-20,y0=Math.min(...ns.map(n=>pos[n.id].y))-58,
        x1=Math.max(...ns.map(n=>pos[n.id].x+NW))+20,y1=Math.max(...ns.map(n=>pos[n.id].y+hOf(sop,n)))+20;
      return `<div class="fx-group ${g[0]==='p'?'page':'lane'}" style="left:${x0}px;top:${y0}px;width:${x1-x0}px;height:${y1-y0}px">
        <div class="fx-group-head" data-group="${esc(g)}" title="Drag to move the whole ${g[0]==='p'?'page':'lane'}">${ic(g[0]==='p'?'smartphone':'split','',12)}<b>${esc(g.slice(2))}</b></div></div>`;
    }).join('');
  }
  function nodeHtml(sop,n,p,st,run){
    const ty=TYPES[n.type]||TYPES.action; const rs=run&&run[n.id];
    const isSel=st.sel===n.id||st.multi.has(n.id);
    const cls=`fx-node k-${n.type} ${n.link?'linked':''} ${PILL(n.type)?'pill':''} ${isSel?'sel':''} ${n.type==='end'&&n.outcome==='rejected'?'rej':''} ${rs?'run-'+rs:''}`;
    const ports=outPorts(n,sop).map(pt=>`<button class="fx-port out" data-port="${pt.i}" style="left:${pt.x}px" aria-label="Connect from ${esc(n.label||ty.label)}">${pt.label?`<span>${esc(pt.label)}</span>`:''}</button>`).join('');
    const runTag=rs?UI.tag(rs==='done'?'Done':rs==='blocked'?'Blocked':rs==='cur'?'Now':'Pending',rs==='done'?'ok':rs==='blocked'?'dng':rs==='cur'?'info':'mut'):'';
    const sum=F.summary(n,sop);
    return `<div class="${cls}" data-node="${n.id}" style="transform:translate(${p.x}px,${p.y}px)">
      ${n.type!=='start'?'<i class="fx-port in"></i>':''}
      <div class="fx-eyebrow">${ic(n.link?'workflow':ty.icon,'',13)}<span>${n.link?'Linked module':ty.label}</span>${runTag}</div>
      <div class="fx-title">${esc(childLabel(n)||'Untitled')}</div>
      ${sum&&!PILL(n.type)?`<div class="fx-sum" title="${esc(sum)}">${n.link?`<a href="${esc(n.link)}" class="fx-link">${esc(sum)} ›</a>`:esc(sum)}</div>`:''}
      ${PILL(n.type)?'':F.settingChips(n,F.latest(sop))}
      ${ports}</div>`;
  }

  F.canvasHtml=function(sop,sel){
    const run=null; /* run state renders only in Try / Operator view */
    const pos=F.positions(sop); const v=view(sop);
    if(F.zoom[sop.id]!=null&&F.zoom[sop.id]!==v.legacyZ){v.legacyZ=F.zoom[sop.id];v.k=F.zoom[sop.id];}
    const st={sel,multi:multiSet(sop),edge:F._edgeSel&&F._edgeSel.sop===sop.id?F._edgeSel.key:null};
    F._run=F._run||{}; F._run[sop.id]=run;
    schedule(sop.id);
    return `<div class="fx-root ${F.palOpen?'pal-open':''}" data-flow="${sop.id}" tabindex="0" style="${bgStyle(v)};--fxk:${v.k}">
      <div class="fx-clip"><div class="fx-world" style="transform:translate(${v.x}px,${v.y}px) scale(${v.k})">
        <div class="fx-groups">${groupsHtml(sop,pos)}</div>
        <svg class="fx-edges" width="1" height="1">${edgesSvg(sop,pos,st)}</svg>
        <div class="fx-pluses">${plusHtml(sop,pos)}</div>
        <div class="fx-nodes">${sop.nodes.map(n=>nodeHtml(sop,n,pos[n.id],st,run)).join('')}</div>
        <svg class="fx-preview" width="1" height="1"><path d=""/></svg>
      </div></div>
      <div class="fx-palette" aria-label="Add step"><div class="fx-palette-h"><span>Add step</span><button class="fx-palt" data-fx="pal" aria-label="${F.palOpen?'Collapse':'Expand'} step list" aria-expanded="${F.palOpen?'true':'false'}" title="${F.palOpen?'Show icons only':'Show names'}">${ic('chevron-right','',14)}</button></div>
        ${PALETTE.map(t=>`<button class="fx-pal k-${t}" data-pal="${t}" aria-label="Add ${TYPES[t].label}" title="${TYPES[t].label}"><i class="fx-pal-ic">${ic(TYPES[t].icon,'',16)}</i><b>${TYPES[t].label}</b></button>`).join('')}
      </div>
      <div class="fx-tools">
        <button class="fx-tb" data-fx="undo" aria-label="Undo" title="Undo ⌘Z" ${hist(sop).undo.length?'':'disabled'}>${ic('undo','',16)}</button>
        <button class="fx-tb" data-fx="redo" aria-label="Redo" title="Redo ⇧⌘Z" ${hist(sop).redo.length?'':'disabled'}><span class="fx-flip">${ic('undo','',16)}</span></button>
        <i class="fx-sep"></i>
        ${F.pages(sop.nodes).length>1?`<select class="fx-jump" data-fxjump aria-label="Jump to page"><option value="">Jump to page</option>${F.pages(sop.nodes).map(p=>`<option>${esc(p)}</option>`).join('')}</select>`:''}
        <button class="fx-tb txt fx-val ${F.validate(sop).length?'bad':'ok'}" data-fx="validate">${ic(F.validate(sop).length?'reject':'check','',15)}Validate${F.validate(sop).length?` <b>${F.validate(sop).length}</b>`:''}</button>
        <i class="fx-sep"></i>
        <button class="fx-tb" data-fx="out" aria-label="Zoom out">${ic('zoomout','',16)}</button>
        <button class="fx-tb" data-fx="in" aria-label="Zoom in">${ic('zoomin','',16)}</button>
        <button class="fx-tb txt" data-fx="fit">Fit</button>
        <button class="fx-tb txt" data-fx="tidy" title="Auto-arrange every step">Tidy</button>
        <button class="fx-tb ${F.miniOff?'':'on'}" data-fx="mini" aria-label="Toggle minimap">${ic('layers','',16)}</button>
        <button class="fx-tb txt" data-fx="help" aria-label="Shortcuts" aria-expanded="false">?</button>
        <button class="fx-tb fx-more" data-fx="more" aria-label="More tools" title="More">${ic('more','',16)}</button>
      </div>
      <div class="fx-help" hidden><div><kbd>Drag</kbd>Pan</div><div><kbd>⌘ scroll</kbd>Zoom</div><div><kbd>Dot ↓</kbd>Connect</div><div><kbd>+</kbd>Insert step</div><div><kbd>Shift click</kbd>Multi-select</div><div><kbd>Del</kbd>Delete</div><div><kbd>⌘D</kbd>Duplicate</div><div><kbd>⌘Z</kbd>Undo</div><div><kbd>⇧⌘Z</kbd>Redo</div><div><kbd>⌘0</kbd>Fit</div></div>
      <div class="fx-issues" hidden></div>
      <div class="fx-mini ${F.miniOff?'hidden':''}"><svg></svg></div>
    </div>`;
  };
  function multiSet(sop){return F.multi[sop.id]||(F.multi[sop.id]=new Set());}
  F.palOpen=false;
  function bgStyle(v){const s=GRID*v.k;return `background-size:${s}px ${s}px;background-position:${v.x}px ${v.y}px`;}

  /* post-render: measure heights, redraw wires, first fit, minimap */
  const pending=new Set();
  function schedule(id){pending.add(id);const run=()=>{if(!pending.size)return;pending.forEach(sid=>mount(sid));pending.clear();};requestAnimationFrame(run);setTimeout(run,60);}
  function rootOf(id){return document.querySelector(`.fx-root[data-flow="${id}"]`);}
  function sopOf(id){return S.get('sops',id);}
  /* per-SOP undo/redo: snapshot of editable SOP state, recorded after every render; quick successive edits coalesce */
  F.hist={};
  const HKEY=id=>'mesha.fxhist.'+id;
  function hist(sop){
    if(F.hist[sop.id])return F.hist[sop.id];
    let h=null; try{h=JSON.parse(sessionStorage.getItem(HKEY(sop.id))||'null');}catch(_){h=null;}
    if(!h||!Array.isArray(h.undo)||!Array.isArray(h.redo))h={undo:[],redo:[],cur:null,key:null};
    return F.hist[sop.id]=h;}
  function saveHist(sop){const h=hist(sop);try{sessionStorage.setItem(HKEY(sop.id),JSON.stringify({undo:h.undo,redo:h.redo,cur:h.cur,key:h.key}));}catch(_){}}
  /* inspector sets F.editKey (node:field) while typing; only consecutive edits of the same text field merge */
  F.editKey=null;
  const snapOf=sop=>JSON.stringify({nodes:sop.nodes,layout:sop.layout||null,title:sop.title,dept:sop.dept,category:sop.category});
  function record(sop){const h=hist(sop),now=snapOf(sop),key=F.editKey; F.editKey=null;
    if(h.cur===null){h.cur=now;saveHist(sop);return;} if(now===h.cur)return;
    if(!(key&&h.key===key)){h.undo.push(h.cur);if(h.undo.length>60)h.undo.shift();}
    h.redo=[];h.cur=now;h.key=key||null;saveHist(sop);}
  function histStep(sop,dir){const h=hist(sop);const from=dir<0?h.undo:h.redo,to=dir<0?h.redo:h.undo;if(!from.length)return;
    to.push(snapOf(sop));const x=JSON.parse(from.pop());sop.nodes=x.nodes;if(x.layout)sop.layout=x.layout;else delete sop.layout;sop.title=x.title;sop.dept=x.dept;sop.category=x.category;
    h.cur=snapOf(sop);h.key=null;saveHist(sop);S.save();if(window.Work&&!sop.nodes.some(n=>n.id===Work.sel[sop.id]))Work.sel[sop.id]=null;App.render();refocus(sop.id);}
  F.undo=id=>{const s=sopOf(id);if(s)histStep(s,-1);}; F.redo=id=>{const s=sopOf(id);if(s)histStep(s,1);};
  F.record=id=>{const s=sopOf(id);if(s)record(s);};
  function mount(id){
    const root=rootOf(id),sop=sopOf(id); if(!root||!sop)return;
    const hb=hist(sop).undo.length,hr=hist(sop).redo.length; record(sop);
    if(hist(sop).undo.length!==hb||hist(sop).redo.length!==hr){const u=root.querySelector('[data-fx=undo]'),r=root.querySelector('[data-fx=redo]');if(u)u.disabled=!hist(sop).undo.length;if(r)r.disabled=!hist(sop).redo.length;}
    syncChrome(root); requestAnimationFrame(()=>{if(root.isConnected)syncChrome(root);}); measure(root,sop);
    const v=view(sop); if(v.fresh){v.fresh=false;fit(root,sop,true);}
    redraw(root,sop);
    const f=F._focus[sop.id]; if(f){delete F._focus[sop.id];focusNode(root,sop,f.id||f,!!f.center);}
    if(!root._ro&&window.ResizeObserver){let w=root.clientWidth,h=root.clientHeight;root._ro=new ResizeObserver(()=>{if(!root.isConnected){root._ro.disconnect();return;}
      if(Math.abs(root.clientWidth-w)<2&&Math.abs(root.clientHeight-h)<2)return;w=root.clientWidth;h=root.clientHeight;syncChrome(root);if(view(sop).auto)fit(root,sop,true);else minimap(root,sop);});root._ro.observe(root);}
  }
  function measure(root,sop){const h=F._h[sop.id]=F._h[sop.id]||{};root.querySelectorAll('.fx-node').forEach(el=>{h[el.dataset.node]=el.offsetHeight;});}
  function redraw(root,sop,pos){
    pos=pos||F.positions(sop);
    const st={sel:(window.Work&&Work.sel[sop.id])||null,multi:multiSet(sop),edge:F._edgeSel&&F._edgeSel.sop===sop.id?F._edgeSel.key:null};
    root.querySelector('.fx-edges').innerHTML=edgesSvg(sop,pos,st);
    root.querySelector('.fx-groups').innerHTML=groupsHtml(sop,pos);
    root.querySelector('.fx-pluses').innerHTML=plusHtml(sop,pos);
    minimap(root,sop,pos);
  }
  function applyView(root,sop){
    const v=view(sop);
    root.querySelector('.fx-world').style.transform=`translate(${v.x}px,${v.y}px) scale(${v.k})`;
    root.style.backgroundSize=`${GRID*v.k}px ${GRID*v.k}px`; root.style.backgroundPosition=`${v.x}px ${v.y}px`;
    if(root._k!==v.k){const was=root._k;root._k=v.k;root.style.setProperty('--fxk',v.k);
      if(was!=null&&(was<1||v.k<1)){cancelAnimationFrame(root._kr);root._kr=requestAnimationFrame(()=>{measure(root,sop);redraw(root,sop);});}}
    const z=root.querySelector('.fx-zoom'); if(z)z.textContent=Math.round(v.k*100)+'%';
    minimap(root,sop);
  }
  /* fit whole flow into the free area (left of minimap, beside or above the palette); tall flows start at the top */
  function fit(root,sop,initial){
    const pos=F.positions(sop); const b=bounds(pos,F.byId(sop.nodes),F._h[sop.id]); const v=view(sop);
    const r=root.getBoundingClientRect(); if(r.width<50||r.height<50)return;
    const pal=root.querySelector('.fx-palette'); const bottomPal=pal.offsetWidth>r.width*.6;
    const narrow=r.width<600, pad=narrow?16:40;
    const padL=bottomPal?pad:pal.offsetWidth+24+pad, padT=topInset(root)+(narrow?12:20), padB=(bottomPal?pal.offsetHeight+20:0)+pad;
    const padR=pad+(F.miniOff||narrow?0:196);
    const aw=Math.max(120,r.width-padL-padR), ah=Math.max(120,r.height-padT-padB);
    const bw=b.x1-b.x0+40, bh=b.y1-b.y0+60;
    /* never shrink to unreadable: >=1280px viewport stays at 100%, otherwise >=75%; the canvas pans for the rest */
    const floor=window.innerWidth>=1280?1:narrow?Math.max(.75,Math.min(1,aw/(NW+40))):.75;
    let k=Math.min(aw/bw,ah/bh,1); k=Math.max(floor,k);
    v.k=k; v.x=padL+(aw-(b.x1-b.x0)*k)/2-b.x0*k;
    if((b.x1-b.x0)*k>aw){const s=sop.nodes.find(n=>n.type==='start');const sp=s&&pos[s.id];v.x=sp?padL+aw/2-(sp.x+NW/2)*k:padL-b.x0*k;}
    v.y=(bh*k>ah)?padT+60*k-b.y0*k:padT+(ah-bh*k)/2+60*k-b.y0*k;
    applyView(root,sop);
  }
  /* bring a node into view (keep zoom) when it is off-screen */
  function topInset(root){const t=root.querySelector('.fx-tools');return t?t.offsetTop+t.offsetHeight+6:54;}
  /* nothing on the canvas renders (or takes clicks) under the floating toolbar */
  function syncChrome(root){
    /* toolbar never clips: Tidy / minimap / shortcuts fold into "More" when the strip is short of room */
    const tools=root.querySelector('.fx-tools');
    if(tools){root.classList.remove('tb-compact','tb-tight');const over=()=>tools.scrollWidth>tools.clientWidth+1;
      if(over()){root.classList.add('tb-compact');if(over())root.classList.add('tb-tight');}}
    root.style.setProperty('--fx-top',topInset(root)+'px');
    root.querySelectorAll('.fx-tools,.fx-palette').forEach(el=>{
      const fade=()=>{const hz=el.scrollWidth>el.clientWidth+1;el.classList.toggle('fade-l',hz&&el.scrollLeft>2);el.classList.toggle('fade-r',hz&&el.scrollLeft<el.scrollWidth-el.clientWidth-2);};
      if(!el._fade){el._fade=1;el.addEventListener('scroll',fade,{passive:true});}
      fade();});
  }
  function focusNode(root,sop,id,center){
    const p=F.positions(sop)[id]; if(!p)return; const v=view(sop); const r=root.getBoundingClientRect();
    const h=hOf(sop,{id,type:(sop.nodes.find(n=>n.id===id)||{}).type});
    const sx=p.x*v.k+v.x, sy=p.y*v.k+v.y, ex=sx+NW*v.k, ey=sy+h*v.k;
    const pal=root.querySelector('.fx-palette'); const bottomPal=pal&&pal.offsetWidth>r.width*.6;
    const left=bottomPal?8:(pal?pal.offsetWidth+24:8), bottom=r.height-(bottomPal?pal.offsetHeight+20:12);
    if(center&&v.k<.8){v.k=.85;}
    const top=topInset(root)+8;
    if(!center&&sx>=left&&ex<=r.width-8&&sy>=top&&ey<=bottom)return;
    v.auto=false;
    if(center){v.x=left+(r.width-left)/2-(p.x+NW/2)*v.k; v.y=top+(bottom-top)/2-(p.y+h/2)*v.k;}
    else{/* smallest pan that brings the step in, so the steps just above it stay on screen */
      const pad=16;
      if(ey>bottom)v.y-=Math.min(ey-bottom+pad,sy-top); else if(sy<top)v.y+=top-sy+pad;
      if(ex>r.width-8)v.x-=Math.min(ex-(r.width-8)+pad,sx-left); else if(sx<left)v.x+=left-sx+pad;}
    applyView(root,sop);
  }
  function zoomAt(root,sop,k,cx,cy){
    const v=view(sop); v.auto=false; k=Math.max(.2,Math.min(2.5,k));
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
    F._edgeSel=null; App.render(); revealInspector();
  }
  /* narrow layouts: the step inspector is a bottom sheet; if it is ever in page flow, bring it into view */
  function revealInspector(){if(window.innerWidth>860)return;const i=document.querySelector('.insp.has-node');if(!i)return;
    const r=i.getBoundingClientRect();if(getComputedStyle(i).position!=='fixed'&&(r.top>window.innerHeight-80||r.bottom<0))i.scrollIntoView({block:'nearest',behavior:'smooth'});}
  F.revealInspector=revealInspector;
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
  function insertAfterSel(sop,type,afterId,edge){
    const a=sop.nodes.find(x=>x.id===afterId); if(!a||a.type==='end')return null;
    const pos=F.positions(sop); persist(sop,pos);
    S.snap('Add step');
    const before=new Set(sop.nodes.map(n=>n.id));
    const n=F.insertAfter(sop,afterId,type,edge); if(!n){S.undo();return null;}
    // push everything below the anchor down to make room, place new nodes under the anchor
    const added=sop.nodes.filter(x=>!before.has(x.id));
    const branchTo=edge!=null&&(a.type==='decision'||a.type==='parallel')?pos[((n.next||[])[0]||{}).to]:null;
    const ap={x:branchTo?branchTo.x:pos[afterId].x,y:pos[afterId].y}; const baseY=ap.y+hOf(sop,a)+RH;
    const need=added.length>1?(estH(n)+RH)*3:estH(n)+RH;
    Object.keys(pos).forEach(id=>{if(id!==afterId&&!added.some(x=>x.id===id)&&pos[id].y>=baseY-10&&Math.abs(pos[id].x-ap.x)<CW*3)pos[id].y+=need;});
    if(type==='parallel'){const lanes=added.filter(x=>x.lane),join=added.find(x=>x.type==='join');
      pos[n.id]={x:ap.x,y:baseY};lanes.forEach((l,i)=>pos[l.id]={x:ap.x+(i-(lanes.length-1)/2)*CW,y:snap(baseY+estH(n)+RH)});
      if(join)pos[join.id]={x:ap.x,y:snap(baseY+estH(n)+RH+estH(lanes[0]||n)+RH)};}
    else added.forEach(x=>pos[x.id]={x:ap.x,y:snap(baseY)});
    persist(sop,pos); return n;
  }
  /* the step that feeds End (lowest on the canvas), or the lowest step with no way out */
  function lastStep(sop,openOnly){
    const pos=F.positions(sop),ends=new Set(sop.nodes.filter(n=>n.type==='end').map(n=>n.id));const c=[];
    sop.nodes.forEach(n=>{if(n.type==='end')return;(n.next||[]).forEach((e,i)=>{if(ends.has(e.to))c.push({id:n.id,edge:i});});if(!(n.next||[]).length)c.push({id:n.id,edge:null,open:1});});
    const y=x=>(pos[x.id]||{y:0}).y;
    c.sort((a,b)=>(a.open||0)-(b.open||0)||y(b)-y(a));
    const r=c[0]||null; return openOnly&&r&&!r.open?null:r;
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
    if(e.target.closest('.fx-tools,.fx-help,.fx-issues,.fx-plus,.fx-link'))return;
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

  /* insert "+" on pointer devices: shown for the hovered connector (or the hovered step's outgoing connectors) */
  function hotPlus(e){
    const root=e.target.closest&&e.target.closest('.fx-root');
    const prev=document.querySelectorAll('.fx-plus.show');
    if(!root){prev.forEach(x=>x.classList.remove('show'));return;}
    let keys=[];const edge=e.target.closest('[data-edge]'),pl=e.target.closest('[data-plus]'),node=e.target.closest('.fx-node');
    if(edge)keys=[edge.dataset.edge];else if(pl)keys=[pl.dataset.plus];
    else if(node){const sop=sopOf(root.dataset.flow);const n=sop&&sop.nodes.find(x=>x.id===node.dataset.node);if(n)keys=(n.next||[]).map((_,i)=>n.id+':'+i);}
    prev.forEach(x=>{if(!keys.includes(x.dataset.plus))x.classList.remove('show');});
    keys.forEach(k=>{const b=root.querySelector(`.fx-plus[data-plus="${k}"]`);if(b)b.classList.add('show');});
  }
  document.addEventListener('pointermove',e=>{
    if(!drag){if(e.pointerType==='mouse')hotPlus(e);return;} const {root,sop}=drag;
    if(drag.kind==='pan'){const v=view(sop);v.auto=false;if(Math.abs(e.clientX-drag.sx)+Math.abs(e.clientY-drag.sy)>3)drag.moved=true;v.x=drag.vx+e.clientX-drag.sx;v.y=drag.vy+e.clientY-drag.sy;applyView(root,sop);return;}
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
        else{const t=lastStep(sop,d.type==='end');
          if(t)n=insertAfterSel(sop,d.type,t.id,t.edge);
          if(!n){const r=root.getBoundingClientRect(),v=view(sop);n=addAt(sop,d.type,(r.width/2-v.x)/v.k,(r.height/2-v.y)/v.k);}}
        if(n){S.save();multiSet(sop).clear();Work.sel[sop.id]=n.id;F._focus[sop.id]=n.id;App.render();refocus(sop.id);revealInspector();}
        return;
      }
      const rr=root.getBoundingClientRect(); if(e.clientX<rr.left||e.clientX>rr.right||e.clientY<rr.top||e.clientY>rr.bottom)return;
      if(document.elementFromPoint(e.clientX,e.clientY)?.closest('.fx-palette'))return;
      const over=document.elementFromPoint(e.clientX,e.clientY); const t=over&&over.closest('.fx-node');
      let n;
      if(t&&root.contains(t))n=insertAfterSel(sop,d.type,t.dataset.node);
      if(!n){const w=world(root,sop,e);n=addAt(sop,d.type,w.x,w.y);}
      S.save();multiSet(sop).clear();Work.sel[sop.id]=n.id;F._focus[sop.id]=n.id;App.render();refocus(sop.id);return;
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
    v.auto=false;
    if(e.shiftKey&&!dx){v.x-=dy;}else{v.x-=dx;v.y-=dy;}
    applyView(root,sop);
  },{passive:false});

  document.addEventListener('change',e=>{
    const j=e.target.closest&&e.target.closest('.fx-root [data-fxjump]'); if(!j||!j.value)return;
    const root=j.closest('.fx-root'),sop=sopOf(root.dataset.flow); if(!sop)return;
    const pos=F.positions(sop); const first=sop.nodes.filter(n=>n.page===j.value&&pos[n.id]).sort((a,b)=>pos[a.id].y-pos[b.id].y)[0];
    if(first){const v=view(sop),r=root.getBoundingClientRect();if(v.k<.8)v.k=.85;v.auto=false;const pal=root.querySelector('.fx-palette');const left=pal.offsetWidth>r.width*.6?0:pal.offsetWidth+24;
      v.x=left+(r.width-left)/2-(pos[first.id].x+NW/2)*v.k; v.y=topInset(root)+24-(pos[first.id].y-66)*v.k; applyView(root,sop);}
    j.value='';
  });
  document.addEventListener('click',e=>{
    const iss=e.target.closest&&e.target.closest('.fx-root [data-fxissue]');
    if(iss){const root=iss.closest('.fx-root'),sop=sopOf(root.dataset.flow);const id=iss.dataset.fxissue;
      if(id&&sop.nodes.some(n=>n.id===id)){multiSet(sop).clear();Work.sel[sop.id]=id;F._focus[sop.id]={id,center:true};App.render();refocus(sop.id);}
      else root.querySelector('.fx-issues').hidden=true;
      return;}
    const plus=e.target.closest&&e.target.closest('.fx-root [data-plus]');
    if(plus){e.stopPropagation();const root=plus.closest('.fx-root'),sop=sopOf(root.dataset.flow);if(!sop)return;
      const [from,i]=plus.dataset.plus.split(':');
      setTimeout(()=>UI.menu(plus,PALETTE.filter(t=>t!=='end').map(t=>({label:TYPES[t].label,icon:TYPES[t].icon,run(){
        const n=insertAfterSel(sop,t,from,+i); if(!n)return; S.save(); multiSet(sop).clear(); Work.sel[sop.id]=n.id; F._focus[sop.id]=n.id; App.render(); refocus(sop.id);}}))),0);
      return;}
    const b=e.target.closest&&e.target.closest('.fx-root [data-fx]'); if(!b)return;
    const root=b.closest('.fx-root'),sop=sopOf(root.dataset.flow); if(!sop)return;
    const r=root.getBoundingClientRect(),v=view(sop),fx=b.dataset.fx;
    if(fx==='in')zoomAt(root,sop,v.k*1.2,r.width/2,r.height/2);
    if(fx==='out')zoomAt(root,sop,v.k/1.2,r.width/2,r.height/2);
    if(fx==='fit')fit(root,sop);
    if(fx==='tidy'){S.snap('Tidy layout');delete sop.layout;S.save();view(sop).fresh=true;App.render();UI.toast('Steps re-arranged',()=>{S.undo();App.render();});}
    if(fx==='undo')F.undo(sop.id);
    if(fx==='redo')F.redo(sop.id);
    if(fx==='validate'){const box=root.querySelector('.fx-issues');const iss=F.validate(sop);
      if(!box.hidden){box.hidden=true;return;}
      box.innerHTML=iss.length?`<div class="fx-issues-h">${iss.length} issue${iss.length>1?'s':''}</div>`+iss.map(i=>`<button data-fxissue="${esc(i.id||'')}">${ic('reject','',14)}<span>${esc(i.msg)}</span></button>`).join(''):`<div class="fx-issues-ok">${ic('check','',15)}Ready to publish</div>`;
      box.hidden=false;}
    if(fx==='help'){const h=root.querySelector('.fx-help');h.hidden=!h.hidden;b.setAttribute('aria-expanded',String(!h.hidden));b.classList.toggle('on',!h.hidden);}
    if(fx==='mini'){F.miniOff=!F.miniOff;root.querySelector('.fx-mini').classList.toggle('hidden',F.miniOff);root.querySelectorAll('[data-fx=mini]').forEach(x=>x.classList.toggle('on',!F.miniOff));minimap(root,sop);}
    if(fx==='pal'){F.palOpen=!F.palOpen;root.classList.toggle('pal-open',F.palOpen);b.setAttribute('aria-expanded',String(F.palOpen));b.setAttribute('aria-label',(F.palOpen?'Collapse':'Expand')+' step list');syncChrome(root);if(view(sop).auto)fit(root,sop,true);}
    if(fx==='more'){e.stopPropagation();const click=k=>{const t=root.querySelector(`.fx-tools [data-fx="${k}"]`);if(t)t.click();};
      const tight=root.classList.contains('tb-tight');
      setTimeout(()=>UI.menu(b,[].concat(tight?[{label:'Zoom in',icon:'zoomin',run:()=>click('in')},{label:'Zoom out',icon:'zoomout',run:()=>click('out')}]:[],
        [{label:'Tidy layout',icon:'workflow',run:()=>click('tidy')},{label:(F.miniOff?'Show':'Hide')+' minimap',icon:'layers',run:()=>click('mini')},{label:'Shortcuts',icon:'help',run:()=>click('help')}])),0);}
  });

  const typing=t=>t&&(t.isContentEditable||/^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName));
  document.addEventListener('keydown',e=>{
    if(e.code==='Space'&&!typing(e.target)&&document.querySelector('.fx-root')){if(!spaceDown){spaceDown=true;document.querySelectorAll('.fx-root').forEach(r=>r.classList.add('space'));}if(e.target.closest&&e.target.closest('.fx-root'))e.preventDefault();return;}
    const root=e.target.closest&&e.target.closest('.fx-root'); if(!root)return;
    const sop=sopOf(root.dataset.flow); if(!sop)return;
    if((e.metaKey||e.ctrlKey)&&(e.key==='z'||e.key==='Z')&&!typing(e.target)){e.preventDefault();e.shiftKey?F.redo(sop.id):F.undo(sop.id);return;}
    if((e.metaKey||e.ctrlKey)&&(e.key==='y')&&!typing(e.target)){e.preventDefault();F.redo(sop.id);return;}
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
  window.addEventListener('resize',()=>{document.querySelectorAll('.fx-root').forEach(r=>{const s=sopOf(r.dataset.flow);if(s){syncChrome(r);minimap(r,s);}});});
  /* re-measure the toolbar once layout settles (stylesheets, fonts, sidebar collapse) */
  const resync=()=>document.querySelectorAll('.fx-root').forEach(r=>syncChrome(r));
  window.addEventListener('load',resync); if(document.fonts&&document.fonts.ready)document.fonts.ready.then(resync);

  /* ---------- structural edits ---------- */
  F.defaults=function(type,sop){
    const base={type,label:'',next:[]};
    if(type==='question')Object.assign(base,{label:'New question',answer:'choice',required:true,options:[{v:'yes',l:'Yes'},{v:'no',l:'No'}]});
    if(type==='evidence')Object.assign(base,{label:'Photo proof',media:['photo'],min:1,max:1});
    if(type==='decision')Object.assign(base,{label:'Check answer',q:'',op:'=',value:''});
    if(type==='approval')Object.assign(base,{label:'Approval',roleId:'',outcomes:['Approve','Send back']});
    if(type==='wait')Object.assign(base,{label:'Wait',amount:1,unit:'days'});
    if(type==='repeat')Object.assign(base,{label:'Repeat check',every:3,everyUnit:'hours',forAmount:1,forUnit:'days',media:['photo']});
    if(type==='child')Object.assign(base,{label:'Follow SOP',sopId:''});
    if(type==='action')Object.assign(base,{label:'New task'});
    if(type==='end')Object.assign(base,{label:'End',outcome:'done'});
    if(type==='parallel')Object.assign(base,{label:'In parallel'});
    return base;
  };
  F.insertAfter=function(sop,afterId,type,edge){
    const nodes=sop.nodes, a=nodes.find(n=>n.id===afterId); if(!a||a.type==='end')return null;
    const ei=edge!=null&&a.next[edge]?edge:0; const oldTo=(a.next[ei]||{}).to;
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
    if(a.next.length)a.next[ei].to=n.id; else a.next=[{to:n.id}];
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
