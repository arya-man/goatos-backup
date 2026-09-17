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
  const estH=n=>PILL(n.type)?60:100+(F.settingIds&&F.settingIds(n).length?34:0);
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
    const ids=F.settingIds(obj).filter(id=>S.get('settings',id));if(!ids.length)return '';
    const lbl=id=>{const st=S.get('settings',id);return st.name+' = '+st.value+(st.unit&&st.unit!=='time'?' '+st.unit:'');};
    const first=ids.find(id=>d[id])||ids[0],rest=ids.filter(id=>id!==first);
    const more=rest.length?`<div class="fx-set fx-set-more ${rest.some(id=>d[id])?'moved':''}" title="${esc(rest.map(lbl).join('\n'))}" aria-label="${rest.length} more settings: ${esc(rest.map(lbl).join('; '))}">+${rest.length}</div>`:'';
    return `<div class="fx-sets">`+[first].map(id=>{const st=S.get('settings',id);const x=d[id];
      return `<div class="fx-set ${x?'moved':''}" title="${x?'Running work keeps '+esc(x.was)+'; publish to use '+esc(x.now):'From business settings'}">Uses setting: ${esc(st.name)} = ${esc(st.value)}${st.unit&&st.unit!=='time'?' '+esc(st.unit):''}${x?` <b>(was ${esc(x.was)})</b>`:''}</div>`;}).join('')+more+`</div>`;};
  F.changed=function(sop){const v=sop.versions[sop.versions.length-1];if(!v)return true;
    F.META.forEach(k=>{if(v[k]===undefined)v[k]=sop[k];});
    F.linkChains(sop);
    return F.stable(v.nodes)!==F.stable(sop.nodes)||F.META.some(k=>v[k]!==sop[k])||F.drift(sop.nodes,v).length>0;};
  /* node label with {value} filled from the live setting (decisions bound to a setting) */
  F.label=function(n){const l=n&&n.label||'';if(!l.includes('{value}'))return l;const vs=n.valueSetting&&S.get('settings',n.valueSetting);return l.replace(/\{value\}/g,vs?vs.value:(n.value!=null?n.value:''));};
  F.roleLabel=function(id){const r=id&&S.get('roles',id);return r?r.name:'';};
  /* approvers of an approval step: its chain (live) or its single role */
  F.approvers=function(n){
    if(n.chainId){const ch=S.get('approvalChains',n.chainId);if(!ch)return [];
      const list=(window.REG&&typeof REG.approversFor==='function')?(REG.approversFor(ch.id)||[]):(ch.steps||[]).map(x=>({label:x.role||x.designation||x.person}));
      return list.map(x=>typeof x==='string'?{label:x}:x).filter(x=>x&&x.label);}
    return n.roleId&&S.get('roles',n.roleId)?[{label:F.roleLabel(n.roleId)}]:[];};
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
        return [a==='Single choice'||a==='Multiple choice'?(o.slice(0,3).join(' / ')+(o.length>3?' …':'')||a):n.answer==='ref'?a+' · '+(n.refColl==='items'&&n.refCat&&window.REG?REG.catPath(n.refCat):(REFS[n.refColl]||'')):a,n.unit,lim,n.required?'required':'',n.reject?'can reject':''].filter(Boolean).join(' · ');}
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
  function childLabel(n){if(n.type!=='child')return F.label(n);const c=S.get('sops',n.sopId);return c?c.title:n.label;}


  /* ---------- stylesheet (owned by this module) ---------- */
  (function(){if(document.querySelector('link[data-flow-css]'))return;const l=document.createElement('link');l.rel='stylesheet';l.href='styles/flow.css?v=20260917u';l.dataset.flowCss='1';l.onload=()=>document.querySelectorAll('.fx-root').forEach(r=>syncChrome(r));document.head.appendChild(l);})();

  /* =====================================================================
     Shared canvas engine. Every flowchart (SOP steps, master SOP stages) is
     a "kind" adapter: nodes/edges read out, mutations in. The engine owns
     rendering, routing, selection, drag, connect/reconnect, marquee,
     context menu, keyboard, history.
     ===================================================================== */
  const KINDS={}; F.kinds=KINDS;
  F.registerKind=(name,k)=>{k.name=name;KINDS[name]=k;};
  const PALETTE=['question','evidence','decision','approval','wait','repeat','child','action','parallel','end'];
  F._h={}; F._opts={}; F.miniOff=true; F._focus={}; F.palOpen=false; F.tool='pan'; F._edgeSel=null;
  const dkey=(K,d)=>K.name+':'+d.id;
  const hOf=(K,d,n)=>((F._h[dkey(K,d)]||{})[n.id])||K.estH(n);
  const inter=(a,b)=>a.x<b.x+b.w&&a.x+a.w>b.x&&a.y<b.y+b.h&&a.y+a.h>b.y;
  function view(key){return F.view[key]||(F.view[key]={x:60,y:40,k:1,fresh:true,auto:true});}
  function multiSet(key){return F.multi[key]||(F.multi[key]=new Set());}

  /* ---------- routing: side choice, spread ports, orthogonal detours, label placement ---------- */
  function route(K,d,pos,k){
    const m={};K.nodes(d).forEach(n=>m[n.id]=n);
    const R=id=>{const p=pos[id];if(!p||!m[id])return null;return {id,x:p.x,y:p.y,w:NW,h:hOf(K,d,m[id])};};
    const rects=Object.keys(m).map(R).filter(Boolean);
    const es=K.edges(d).map(e=>Object.assign({},e)).filter(e=>R(e.from)&&R(e.to));
    es.forEach(e=>{const A=R(e.from),B=R(e.to);e.A=A;e.B=B;
      if(e.from===e.to){e.ss='r';e.ts='t';e.self=true;return;}
      if(B.y>=A.y+A.h+16){e.ss='b';e.ts='t';}
      else if(B.x>=A.x+A.w+16){e.ss='r';e.ts='l';}
      else if(B.x+B.w<=A.x-16){e.ss='l';e.ts='r';}
      else{const right=(B.x+B.w/2)>=(A.x+A.w/2);e.ss=e.ts=right?'r':'l';e.loop=true;}});
    const slots={};const add=(id,side,e,end)=>{const kk=id+'|'+side;(slots[kk]=slots[kk]||[]).push({e,end});};
    es.forEach(e=>{add(e.from,e.ss,e,'s');add(e.to,e.ts,e,'t');});
    Object.entries(slots).forEach(([kk,list])=>{
      const [id,side]=kk.split('|');const r=R(id);const hz=side==='t'||side==='b';
      const oth=it=>{const o=it.end==='s'?it.e.B:it.e.A;return hz?o.x+o.w/2:o.y+o.h/2;};
      list.sort((a,b)=>oth(a)-oth(b)||(a.end<b.end?-1:1));
      const n=list.length,len=hz?r.w:r.h,span=Math.min(len-28,(n-1)*(hz?88:26));
      list.forEach((it,i)=>{const off=n===1?len/2:len/2-span/2+span*i/(n-1);
        const pt=side==='t'?[r.x+off,r.y]:side==='b'?[r.x+off,r.y+r.h]:side==='l'?[r.x,r.y+off]:[r.x+r.w,r.y+off];
        if(it.end==='s')it.e.sp=pt;else it.e.tp=pt;});});
    const segHit=(p,q,skip)=>{const r={x:Math.min(p[0],q[0])-1,y:Math.min(p[1],q[1])-1,w:Math.abs(p[0]-q[0])+2,h:Math.abs(p[1]-q[1])+2};
      return rects.some(o=>!skip.includes(o.id)&&inter(r,{x:o.x-8,y:o.y-8,w:o.w+16,h:o.h+16}));};
    const pathHit=(pts,skip)=>pts.some((p,i)=>i&&segHit(pts[i-1],p,skip));
    let loopIx=0;
    es.forEach(e=>{const [sx,sy]=e.sp,[tx,ty]=e.tp;const skip=[e.from,e.to];let pts;
      if(e.self){const X=e.A.x+e.A.w+30,Y=e.A.y-26;pts=[[sx,sy],[X,sy],[X,Y],[tx,Y],[tx,ty]];}
      else if(e.ss==='b'){
        if(Math.abs(sx-tx)<1&&!segHit([sx,sy],[tx,ty],skip))pts=[[sx,sy],[tx,ty]];
        else{const cands=ty-sy<60?[(sy+ty)/2]:[sy+24,ty-24,(sy+ty)/2];
          for(const my of cands){const p=[[sx,sy],[sx,my],[tx,my],[tx,ty]];if(!pathHit(p,skip)){pts=p;break;}}
          if(!pts){/* detour round the column of obstacles */
            const ys=[sy,ty];const band=rects.filter(o=>!skip.includes(o.id)&&o.y<ty&&o.y+o.h>sy);
            const right=Math.max(sx,tx,...band.map(o=>o.x+o.w))+30+10*(loopIx++%4),left=Math.min(sx,tx,...band.map(o=>o.x))-30;
            const cx=Math.abs(right-sx)<=Math.abs(sx-left)?right:left;
            pts=[[sx,sy],[sx,ys[0]+22],[cx,ys[0]+22],[cx,ty-22],[tx,ty-22],[tx,ty]];}}
      }else if(e.loop){
        const lo=Math.min(sy,ty),hi=Math.max(sy,ty);const band=rects.filter(o=>o.y<hi&&o.y+o.h>lo);
        const X=e.ss==='r'?Math.max(...band.map(o=>o.x+o.w))+30+12*(loopIx++%4):Math.min(...band.map(o=>o.x))-30-12*(loopIx++%4);
        pts=[[sx,sy],[X,sy],[X,ty],[tx,ty]];
      }else{
        if(Math.abs(sy-ty)<1)pts=[[sx,sy],[tx,ty]];
        else{const mx=(sx+tx)/2;pts=[[sx,sy],[mx,sy],[mx,ty],[tx,ty]];}
      }
      e.pts=pts;e.d=ortho(pts);});
    /* labels: pill on the longest free stretch; never over a node or another pill */
    const f=Math.max(1,1/(k||1));const pills=[];
    const fits=r=>!rects.some(o=>inter(r,{x:o.x-4,y:o.y-4,w:o.w+8,h:o.h+8}))&&!pills.some(p=>inter(r,{x:p.x-4,y:p.y-4,w:p.w+8,h:p.h+8}));
    const segsOf=e=>e.pts.slice(1).map((p,i)=>({a:e.pts[i],b:p,len:Math.hypot(p[0]-e.pts[i][0],p[1]-e.pts[i][1])})).sort((x,y)=>y.len-x.len);
    es.forEach(e=>{if(!e.label)return;const w=(String(e.label).length*6.7+18)*f,h=20*f;let best=null;
      for(const s of segsOf(e)){if(s.len<22)continue;for(const t of [.5,.35,.65,.2,.8]){const cx=s.a[0]+(s.b[0]-s.a[0])*t,cy=s.a[1]+(s.b[1]-s.a[1])*t;
        const r={x:cx-w/2,y:cy-h/2,w,h};if(fits(r)){best={cx,cy,r};break;}}if(best)break;}
      if(!best){/* no free stretch (e.g. short hop between side-by-side cards): nearest clear spot beside the line's midpoint */
        const s=segsOf(e)[0];const mx=(s.a[0]+s.b[0])/2,my=(s.a[1]+s.b[1])/2;const cands=[];
        for(let dy=-160;dy<=160;dy+=8)for(let dx=-160;dx<=160;dx+=8)cands.push([dx,dy,Math.hypot(dx,dy)]);
        cands.sort((a,b)=>a[2]-b[2]);
        for(const [dx,dy] of cands){const cx=mx+dx,cy=my+dy,r={x:cx-w/2,y:cy-h/2,w,h};if(fits(r)){best={cx,cy,r};break;}}
        if(!best)best={cx:mx,cy:my,r:{x:mx-w/2,y:my-h/2,w,h}};}
      pills.push(best.r);e.lp=best;});
    es.forEach(e=>{const s=segsOf(e)[0];if(!s||s.len<76)return;
      for(const t of [.5,.25,.75]){const cx=s.a[0]+(s.b[0]-s.a[0])*t,cy=s.a[1]+(s.b[1]-s.a[1])*t;if(!pills.some(p=>inter({x:cx-14,y:cy-14,w:28,h:28},p))){e.pp=[cx,cy];break;}}});
    return es;
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
  const edgeSelKey=(K,d)=>F._edgeSel&&F._edgeSel.doc===dkey(K,d)?F._edgeSel.key:null;
  function edgesSvg(K,d,es){
    const sel=edgeSelKey(K,d);let wires='',labels='',handles='';
    es.forEach(e=>{const on=sel===e.key;
      wires+=`<g class="fx-eg ${on?'sel':''} ${e.cls||''}" data-eg="${esc(e.key)}"><path class="fx-wire ${e.cls||''}" d="${e.d}" marker-end="url(#fxah${on?'s':''})"/><path class="fx-wire-hit" data-edge="${esc(e.key)}" d="${e.d}"/></g>`;
      if(e.lp){const r=e.lp.r;labels+=`<g class="fx-elabel ${on?'sel':''} ${e.cls||''}" data-edge="${esc(e.key)}"><rect x="${r.x}" y="${r.y}" width="${r.w}" height="${r.h}" rx="${r.h/2}"/><text x="${e.lp.cx}" y="${e.lp.cy}" dominant-baseline="central">${esc(e.label)}</text></g>`;}
      handles+=`<g class="fx-ehs ${on?'on':''}" data-ehs="${esc(e.key)}"><circle class="fx-ehit" data-eh="s" data-edge="${esc(e.key)}" cx="${e.sp[0]}" cy="${e.sp[1]}" r="12"/><circle class="fx-ehit" data-eh="t" data-edge="${esc(e.key)}" cx="${e.tp[0]}" cy="${e.tp[1]}" r="12"/><circle class="fx-eh" data-eh="s" data-edge="${esc(e.key)}" cx="${e.sp[0]}" cy="${e.sp[1]}" r="6"/><circle class="fx-eh" data-eh="t" data-edge="${esc(e.key)}" cx="${e.tp[0]}" cy="${e.tp[1]}" r="6"/></g>`;});
    return {edges:`<defs><marker id="fxah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse"><path d="M0,1 L10,5 L0,9 z" class="fx-ah"/></marker><marker id="fxahs" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse"><path d="M0,1 L10,5 L0,9 z" class="fx-ah sel"/></marker></defs>`+wires+labels,handles};
  }
  function plusHtml(K,d,es){if(!K.insertOnEdge)return '';const sel=edgeSelKey(K,d);
    return es.filter(e=>e.pp).map(e=>`<button class="fx-plus ${sel===e.key?'show':''}" data-plus="${esc(e.key)}" style="left:${e.pp[0]}px;top:${e.pp[1]}px" aria-label="Insert step here"></button>`).join('');}

  /* ---------- SOP kind ---------- */
  function sopGroupsHtml(sop,pos){
    const K=KINDS.sop;const groups={};
    sop.nodes.forEach(n=>{const g=n.page?'p:'+n.page:(n.lane?'l:'+n.lane:null);if(!g||!pos[n.id])return;(groups[g]=groups[g]||[]).push(n);});
    return Object.entries(groups).map(([g,ns])=>{
      const x0=Math.min(...ns.map(n=>pos[n.id].x))-20,y0=Math.min(...ns.map(n=>pos[n.id].y))-58,
        x1=Math.max(...ns.map(n=>pos[n.id].x+NW))+20,y1=Math.max(...ns.map(n=>pos[n.id].y+hOf(K,sop,n)))+20;
      return `<div class="fx-group ${g[0]==='p'?'page':'lane'}" style="left:${x0}px;top:${y0}px;width:${x1-x0}px;height:${y1-y0}px">
        <div class="fx-group-head" data-group="${esc(g)}" title="Drag to move the whole ${g[0]==='p'?'page':'lane'}">${ic(g[0]==='p'?'smartphone':'split','',12)}<b>${esc(g.slice(2))}</b></div></div>`;
    }).join('');
  }
  function persist(K,d,pos){d.layout={};K.nodes(d).forEach(n=>{if(pos[n.id])d.layout[n.id]={x:pos[n.id].x,y:pos[n.id].y};});S.save();}
  const nfind=(sop,id)=>sop.nodes.find(x=>x.id===id);
  const ekey=key=>{const i=key.lastIndexOf(':');return [key.slice(0,i),+key.slice(i+1)];};
  function sopBranchLabel(n){const L=n.next.length;
    if(n.type==='decision')return L===0?'Yes':L===1?'No':'Branch '+(L+1);
    if(n.type==='parallel')return 'Lane '+String.fromCharCode(65+L);
    if(n.type==='approval')return (n.outcomes||[])[L]||'Outcome '+(L+1);
    return '';}
  const MULTI=t=>t==='decision'||t==='parallel'||t==='approval';
  function sopAddAt(sop,type,x,y){
    const pos=F.positions(sop);const n=Object.assign({id:F.nid(sop.nodes)},F.defaults(type,sop));sop.nodes.push(n);
    pos[n.id]={x:snap(x-NW/2),y:snap(y-30)};persist(KINDS.sop,sop,pos);return n;}
  function insertAfterSel(sop,type,afterId,edge){
    const a=nfind(sop,afterId); if(!a||a.type==='end')return null;
    const pos=F.positions(sop); persist(KINDS.sop,sop,pos);
    const before=new Set(sop.nodes.map(n=>n.id));
    const n=F.insertAfter(sop,afterId,type,edge); if(!n)return null;
    const added=sop.nodes.filter(x=>!before.has(x.id));
    const branchTo=edge!=null&&MULTI(a.type)?pos[((n.next||[])[0]||{}).to]:null;
    const ap={x:branchTo?branchTo.x:pos[afterId].x,y:pos[afterId].y}; const baseY=ap.y+hOf(KINDS.sop,sop,a)+RH;
    const need=added.length>1?(estH(n)+RH)*3:estH(n)+RH;
    Object.keys(pos).forEach(id=>{if(id!==afterId&&!added.some(x=>x.id===id)&&pos[id].y>=baseY-10&&Math.abs(pos[id].x-ap.x)<CW*3)pos[id].y+=need;});
    if(type==='parallel'){const lanes=added.filter(x=>x.lane),join=added.find(x=>x.type==='join');
      pos[n.id]={x:ap.x,y:baseY};lanes.forEach((l,i)=>pos[l.id]={x:ap.x+(i-(lanes.length-1)/2)*CW,y:snap(baseY+estH(n)+RH)});
      if(join)pos[join.id]={x:ap.x,y:snap(baseY+estH(n)+RH+estH(lanes[0]||n)+RH)};}
    else added.forEach(x=>pos[x.id]={x:ap.x,y:snap(baseY)});
    persist(KINDS.sop,sop,pos); return n;
  }
  function lastStep(sop,openOnly){
    const pos=F.positions(sop),ends=new Set(sop.nodes.filter(n=>n.type==='end').map(n=>n.id));const c=[];
    sop.nodes.forEach(n=>{if(n.type==='end')return;(n.next||[]).forEach((e,i)=>{if(ends.has(e.to))c.push({id:n.id,edge:i});});if(!(n.next||[]).length)c.push({id:n.id,edge:null,open:1});});
    const y=x=>(pos[x.id]||{y:0}).y;
    c.sort((a,b)=>(a.open||0)-(b.open||0)||y(b)-y(a));
    const r=c[0]||null; return openOnly&&r&&!r.open?null:r;
  }
  F.registerKind('sop',{
    get:id=>S.get('sops',id), nodes:d=>d.nodes, estH,
    edges:d=>{const out=[];d.nodes.forEach(n=>(n.next||[]).forEach((e,i)=>out.push({key:n.id+':'+i,from:n.id,to:e.to,label:e.when||''})));return out;},
    positions:d=>F.positions(d),
    getSel:d=>window.Work?Work.sel[d.id]||null:null, setSel:(d,id)=>{if(window.Work)Work.sel[d.id]=id||null;},
    snapshot:d=>JSON.stringify({nodes:d.nodes,layout:d.layout||null,title:d.title,dept:d.dept,category:d.category,speciesId:d.speciesId||null}),
    restore(d,s){const x=JSON.parse(s);d.nodes=x.nodes;if(x.layout)d.layout=x.layout;else delete d.layout;d.title=x.title;d.dept=x.dept;d.category=x.category;if(x.speciesId)d.speciesId=x.speciesId;else delete d.speciesId;},
    palette:PALETTE, typeLabel:t=>TYPES[t].label, typeIcon:t=>TYPES[t].icon, noun:'step',
    groups:sopGroupsHtml, pages:d=>F.pages(d.nodes),
    validate:d=>F.validate(d),
    nodeClass:n=>`k-${n.type} ${n.link?'linked':''} ${PILL(n.type)?'pill':''} ${n.type==='end'&&n.outcome==='rejected'?'rej':''}`,
    nodeInner(sop,n){const ty=TYPES[n.type]||TYPES.action;const sum=F.summary(n,sop);
      return `<div class="fx-eyebrow">${ic(n.link?'workflow':ty.icon,'',13)}<span>${n.link?'Linked module':ty.label}</span></div>
      <div class="fx-title">${esc(childLabel(n)||'Untitled')}</div>
      ${sum&&!PILL(n.type)?`<div class="fx-sum" title="${esc(sum)}">${n.link?`<a href="${esc(n.link)}" class="fx-link">${esc(sum)} ›</a>`:esc(sum)}</div>`:''}
      ${PILL(n.type)?'':F.settingChips(n,F.latest(sop))}`;},
    connect(sop,from,to){const n=nfind(sop,from),t=nfind(sop,to);
      if(!n||!t)return 'Cannot connect';if(from===to)return 'A step cannot lead to itself';
      if(n.type==='end')return 'An End step has no next step';if(t.type==='start')return 'Nothing can lead into Start';
      n.next=n.next||[];
      if(MULTI(n.type)){if(n.type!=='decision'&&n.next.some(e=>e.to===to))return 'Already connected';n.next.push(Object.assign({to},sopBranchLabel(n)?{when:sopBranchLabel(n)}:{}));return true;}
      if(n.next.some(e=>e.to===to))return 'Already connected';
      if(n.next.length){n.next[0].to=to;n.next=n.next.slice(0,1);return 'moved';}
      n.next=[{to}];return true;},
    reconnect(sop,key,end,id){const [fid,i]=ekey(key);const n=nfind(sop,fid);const e=n&&n.next[i];if(!e)return 'Connection not found';
      if(end==='t'){const t=nfind(sop,id);if(!t)return 'Cannot connect';if(t.type==='start')return 'Nothing can lead into Start';if(id===fid)return 'A step cannot lead to itself';e.to=id;return true;}
      const m=nfind(sop,id);if(!m)return 'Cannot connect';if(id===fid)return true;if(m.type==='end')return 'An End step has no next step';if(id===e.to)return 'A step cannot lead to itself';
      n.next.splice(i,1);m.next=m.next||[];
      if(MULTI(m.type))m.next.push(Object.assign({to:e.to},e.when||sopBranchLabel(m)?{when:e.when||sopBranchLabel(m)}:{}));else m.next=[{to:e.to}];
      return true;},
    deleteEdge(sop,key){const [fid,i]=ekey(key);const n=nfind(sop,fid);if(n)n.next.splice(i,1);},
    edgeText(sop,key){const [fid,i]=ekey(key);const n=nfind(sop,fid);return n&&n.next[i]?n.next[i].when||'':'';},
    setEdgeText(sop,key,t){const [fid,i]=ekey(key);const n=nfind(sop,fid);const e=n&&n.next[i];if(!e)return;if(t)e.when=t;else delete e.when;
      if(n.type==='parallel'){const l=nfind(sop,e.to);if(l&&l.lane!==undefined)l.lane=t;}},
    removeNodes(sop,ids){let c=0;ids.forEach(id=>{if(F.remove(sop,id)){c++;if(sop.layout)delete sop.layout[id];}});return c;},
    disconnect(sop,ids){const s=new Set(ids);sop.nodes.forEach(n=>{n.next=(n.next||[]).filter(e=>!(s.has(n.id)||s.has(e.to)));});},
    duplicate(sop,ids){const pos=F.positions(sop);const out=[];
      ids.forEach(id=>{const n=nfind(sop,id);if(!n||n.type==='start')return;
        const c=JSON.parse(JSON.stringify(n));c.id=F.nid(sop.nodes);c.next=[];sop.nodes.push(c);pos[c.id]={x:pos[id].x+40,y:pos[id].y+40};out.push(c.id);});
      persist(KINDS.sop,sop,pos);return out;},
    addAt:(sop,type,x,y)=>sopAddAt(sop,type,x,y).id,
    addAfter:(sop,type,afterId)=>{const n=insertAfterSel(sop,type,afterId);return n&&n.id;},
    canAddAfter:(sop,id)=>{const n=nfind(sop,id);return n&&n.type!=='end';},
    insertOnEdge(sop,key,type){const [fid,i]=ekey(key);const n=insertAfterSel(sop,type,fid,i);return n&&n.id;},
    paletteClick(sop,type,center){const after=Work.sel[sop.id];const a=nfind(sop,after);let n;
      if(a&&a.type!=='end')n=insertAfterSel(sop,type,after);
      else{const t=lastStep(sop,type==='end');if(t)n=insertAfterSel(sop,type,t.id,t.edge);if(!n)n=sopAddAt(sop,type,center.x,center.y);}
      return n&&n.id;},
    paletteDrop(sop,type,w,overId){let n=overId?insertAfterSel(sop,type,overId):null;if(!n)n=sopAddAt(sop,type,w.x,w.y);return n.id;},
    starts:d=>d.nodes.filter(n=>n.type==='start').map(n=>n.id),
    onSelected(){F.revealInspector();}
  });

  /* ---------- rendering ---------- */
  function nodeHtml(K,d,n,p,st,i){
    const isSel=st.sel===n.id||st.multi.has(n.id);
    return `<div class="fx-node ${K.nodeClass(n,d)} ${isSel?'sel':''}" data-node="${esc(n.id)}" style="transform:translate(${p.x}px,${p.y}px)">
      ${K.nodeInner(d,n,i,F._opts[dkey(K,d)]||{})}
      <i class="fx-anc" data-anc="t"></i><i class="fx-anc" data-anc="r"></i><i class="fx-anc" data-anc="b"></i><i class="fx-anc" data-anc="l"></i></div>`;
  }
  F.kindCanvas=function(kind,d,sel,opts){
    const K=KINDS[kind];const key=dkey(K,d);F._opts[key]=opts||{};
    const pos=K.positions(d); const v=view(key);
    const st={sel,multi:multiSet(key)};
    const es=route(K,d,pos,v.k);const iss=K.validate(d);
    schedule(key);
    const pages=K.pages?K.pages(d):[];
    return `<div class="fx-root ${F.palOpen?'pal-open':''} tool-${F.tool} ${v.k<.9?'zoomed-out':''}" data-flow="${esc(d.id)}" data-kind="${kind}" tabindex="0" style="${bgStyle(v)};--fxk:${v.k}">
      <div class="fx-clip"><div class="fx-world" style="transform:translate(${v.x}px,${v.y}px) scale(${v.k})">
        <div class="fx-groups">${K.groups?K.groups(d,pos):''}</div>
        <svg class="fx-edges" width="1" height="1">${edgesSvg(K,d,es).edges}</svg>
        <div class="fx-pluses">${plusHtml(K,d,es)}</div>
        <div class="fx-nodes">${K.nodes(d).map((n,i)=>nodeHtml(K,d,n,pos[n.id],st,i)).join('')}</div>
        <svg class="fx-handles" width="1" height="1">${edgesSvg(K,d,es).handles}</svg>
        <svg class="fx-preview" width="1" height="1"><path d=""/></svg>
      </div></div>
      <div class="fx-marq" hidden></div>
      <div class="fx-palette" aria-label="Add ${K.noun}"><div class="fx-palette-h"><span>Add ${K.noun}</span><button class="fx-palt" data-fx="pal" aria-label="${F.palOpen?'Collapse':'Expand'} list" aria-expanded="${F.palOpen?'true':'false'}">${ic('chevron-right','',14)}</button></div>
        ${K.palette.map(t=>`<button class="fx-pal k-${t}" data-pal="${t}" aria-label="Add ${K.typeLabel(t)}" title="${K.typeLabel(t)}"><i class="fx-pal-ic">${ic(K.typeIcon(t),'',16)}</i><b>${K.typeLabel(t)}</b></button>`).join('')}
      </div>
      <div class="fx-tools">
        <button class="fx-tb ${F.tool==='select'?'on':''}" data-fx="tool-select" aria-label="Select tool" title="Select: drag to box-select (V)"><svg class="ic" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M5 3l14 8-6 1.5L10 19z"/></svg></button>
        <button class="fx-tb ${F.tool==='pan'?'on':''}" data-fx="tool-pan" aria-label="Pan tool" title="Pan: drag to move the canvas (H) · Shift-drag box-selects"><svg class="ic" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 13V5.5a1.5 1.5 0 013 0V12M11 11.5v-7a1.5 1.5 0 013 0V12M14 6.5a1.5 1.5 0 013 0V13M17 8.5a1.5 1.5 0 013 0V15a6 6 0 01-6 6h-2a6 6 0 01-4.9-2.5L4 14a1.5 1.5 0 012.5-1.7L8 14"/></svg></button>
        <i class="fx-sep"></i>
        <button class="fx-tb" data-fx="undo" aria-label="Undo" title="Undo ⌘Z" ${hist(K,d).undo.length?'':'disabled'}>${ic('undo','',16)}</button>
        <button class="fx-tb" data-fx="redo" aria-label="Redo" title="Redo ⇧⌘Z" ${hist(K,d).redo.length?'':'disabled'}><span class="fx-flip">${ic('undo','',16)}</span></button>
        <i class="fx-sep"></i>
        ${pages.length>1?`<select class="fx-jump" data-fxjump aria-label="Jump to page"><option value="">Jump to page</option>${pages.map(p=>`<option>${esc(p)}</option>`).join('')}</select>`:''}
        <button class="fx-tb txt fx-val ${iss.length?'bad':'ok'}" data-fx="validate" aria-label="Validate${iss.length?' · '+iss.length+' issues':''}" title="Validate">${ic(iss.length?'reject':'check','',15)}<span class="fx-vl">Validate</span><span class="fx-vs">Check</span>${iss.length?` <b>${iss.length}</b>`:''}</button>
        <span class="sp"></span>
        <button class="fx-tb" data-fx="out" aria-label="Zoom out">${ic('zoomout','',16)}</button>
        <span class="fx-zoom">${Math.round(v.k*100)}%</span>
        <button class="fx-tb" data-fx="in" aria-label="Zoom in">${ic('zoomin','',16)}</button>
        <button class="fx-tb txt" data-fx="fit">Fit</button>
        <button class="fx-tb txt" data-fx="tidy" title="Auto-arrange">Tidy</button>
        <button class="fx-tb ${F.miniOff?'':'on'}" data-fx="mini" aria-label="Toggle minimap">${ic('layers','',16)}</button>
        <button class="fx-tb txt" data-fx="help" aria-label="Shortcuts" aria-expanded="false">?</button>
        <button class="fx-tb fx-more" data-fx="more" aria-label="More tools">${ic('more','',16)}</button>
      </div>
      <div class="fx-help" hidden><div><kbd>Drag canvas</kbd>Pan (V/H switch tool)</div><div><kbd>Shift drag</kbd>Box select</div><div><kbd>⌘ scroll / pinch</kbd>Zoom</div><div><kbd>Dot on a box</kbd>Drag to connect</div><div><kbd>Arrow end</kbd>Drag to reconnect</div><div><kbd>Double-click label</kbd>Rename branch</div><div><kbd>Right-click</kbd>More actions</div><div><kbd>Del</kbd>Delete</div><div><kbd>⌘D</kbd>Duplicate</div><div><kbd>⌘Z / ⇧⌘Z</kbd>Undo / redo</div><div><kbd>Esc</kbd>Cancel drag</div><div><kbd>⌘0</kbd>Fit</div></div>
      <div class="fx-issues" hidden></div>
      <div class="fx-mini ${F.miniOff?'hidden':''}"><svg></svg></div>
    </div>`;
  };
  F.canvasHtml=function(sop,sel){return F.kindCanvas('sop',sop,sel);};
  function bgStyle(v){const s=GRID*v.k;return `background-size:${s}px ${s}px;background-position:${v.x}px ${v.y}px`;}

  /* ---------- mount / history ---------- */
  const pending=new Set();
  function schedule(key){pending.add(key);const run=()=>{if(!pending.size)return;pending.forEach(k=>mount(k));pending.clear();};requestAnimationFrame(run);setTimeout(run,60);}
  function rootOfKey(key){const [kind,...rest]=key.split(':');const id=rest.join(':');return [...document.querySelectorAll('.fx-root')].find(r=>r.dataset.kind===kind&&r.dataset.flow===id)||null;}
  function ctxOf(root){const K=KINDS[root.dataset.kind];const d=K&&K.get(root.dataset.flow);return d?{K,d,key:dkey(K,d)}:null;}
  F.hist={};
  const HKEY=key=>'mesha.fxhist.'+key;
  function hist(K,d){const key=dkey(K,d);
    if(F.hist[key])return F.hist[key];
    let h=null; try{h=JSON.parse(sessionStorage.getItem(HKEY(key))||'null');}catch(_){h=null;}
    if(!h||!Array.isArray(h.undo)||!Array.isArray(h.redo))h={undo:[],redo:[],cur:null,key:null};
    return F.hist[key]=h;}
  function saveHist(K,d){const h=hist(K,d);try{sessionStorage.setItem(HKEY(dkey(K,d)),JSON.stringify(h));}catch(_){}}
  F.editKey=null;
  function record(K,d){const h=hist(K,d),now=K.snapshot(d),ek=F.editKey; F.editKey=null;
    if(h.cur===null){h.cur=now;saveHist(K,d);return;} if(now===h.cur)return;
    if(!(ek&&h.key===ek)){h.undo.push(h.cur);if(h.undo.length>80)h.undo.shift();}
    h.redo=[];h.cur=now;h.key=ek||null;saveHist(K,d);}
  function histStep(K,d,dir){const h=hist(K,d);const from=dir<0?h.undo:h.redo,to=dir<0?h.redo:h.undo;if(!from.length)return;
    to.push(K.snapshot(d));K.restore(d,from.pop());h.cur=K.snapshot(d);h.key=null;saveHist(K,d);S.save();
    const key=dkey(K,d);const ids=new Set(K.nodes(d).map(n=>n.id));if(!ids.has(K.getSel(d)))K.setSel(d,null);
    const ms=multiSet(key);[...ms].forEach(x=>{if(!ids.has(x))ms.delete(x);});F._edgeSel=null;
    App.render();refocus(key);}
  F.undo=id=>{const d=KINDS.sop.get(id);if(d)histStep(KINDS.sop,d,-1);}; F.redo=id=>{const d=KINDS.sop.get(id);if(d)histStep(KINDS.sop,d,1);};
  F.record=id=>{const d=KINDS.sop.get(id);if(d)record(KINDS.sop,d);};
  function mount(key){
    const root=rootOfKey(key); if(!root)return; const c=ctxOf(root); if(!c)return; const {K,d}=c;
    record(K,d);
    const clip=root.querySelector('.fx-clip');if(clip&&!clip._lock){clip._lock=1;clip.addEventListener('scroll',()=>{clip.scrollTop=0;clip.scrollLeft=0;});}
    const u=root.querySelector('[data-fx=undo]'),r=root.querySelector('[data-fx=redo]');if(u)u.disabled=!hist(K,d).undo.length;if(r)r.disabled=!hist(K,d).redo.length;
    syncChrome(root); requestAnimationFrame(()=>{if(root.isConnected)syncChrome(root);}); measure(root,K,d);
    const v=view(key); if(v.fresh){v.fresh=false;fit(root,K,d,true);}
    redraw(root,K,d);
    const f=F._focus[key]||F._focus[d.id]; if(f){delete F._focus[key];delete F._focus[d.id];focusNode(root,K,d,f.id||f,!!f.center);}
    if(!root._ro&&window.ResizeObserver){let w=root.clientWidth,h=root.clientHeight;root._ro=new ResizeObserver(()=>{if(!root.isConnected){root._ro.disconnect();return;}
      if(Math.abs(root.clientWidth-w)<2&&Math.abs(root.clientHeight-h)<2)return;w=root.clientWidth;h=root.clientHeight;syncChrome(root);if(view(key).auto)fit(root,K,d,true);else minimap(root,K,d);});root._ro.observe(root);}
  }
  function measure(root,K,d){const h=F._h[dkey(K,d)]=F._h[dkey(K,d)]||{};root.querySelectorAll('.fx-node').forEach(el=>{h[el.dataset.node]=el.offsetHeight;});}
  function redraw(root,K,d,pos,light){
    pos=pos||K.positions(d);const es=route(K,d,pos,view(dkey(K,d)).k);
    const sv=edgesSvg(K,d,es);root.querySelector('.fx-edges').innerHTML=sv.edges;root.querySelector('.fx-handles').innerHTML=sv.handles;
    root.querySelector('.fx-groups').innerHTML=K.groups?K.groups(d,pos):'';
    if(!light){root.querySelector('.fx-pluses').innerHTML=plusHtml(K,d,es);minimap(root,K,d,pos);}
    return es;
  }
  function rerender(root){const c=ctxOf(root);if(!c)return;const had=document.activeElement===root;
    const html=F.kindCanvas(c.K.name,c.d,c.K.getSel(c.d),F._opts[c.key]);root.outerHTML=html;if(had)refocus(c.key);}
  function applyView(root,K,d){
    const key=dkey(K,d);const v=view(key);
    root.querySelector('.fx-world').style.transform=`translate(${v.x}px,${v.y}px) scale(${v.k})`;
    root.style.backgroundSize=`${GRID*v.k}px ${GRID*v.k}px`; root.style.backgroundPosition=`${v.x}px ${v.y}px`;
    if(root._k!==v.k){const was=root._k;root._k=v.k;root.style.setProperty('--fxk',v.k);root.classList.toggle('zoomed-out',v.k<.9);
      if(was!=null){cancelAnimationFrame(root._kr);root._kr=requestAnimationFrame(()=>{measure(root,K,d);redraw(root,K,d);});}}
    const z=root.querySelector('.fx-zoom'); if(z)z.textContent=Math.round(v.k*100)+'%';
    minimap(root,K,d);
  }
  function kbounds(K,d,pos){const ids=K.nodes(d).map(n=>n).filter(n=>pos[n.id]);if(!ids.length)return {x0:0,y0:0,x1:NW,y1:100};
    let x0=1e9,y0=1e9,x1=-1e9,y1=-1e9;ids.forEach(n=>{const p=pos[n.id],h=hOf(K,d,n);x0=Math.min(x0,p.x);y0=Math.min(y0,p.y);x1=Math.max(x1,p.x+NW);y1=Math.max(y1,p.y+h);});
    return {x0,y0,x1,y1};}
  function palInset(root,r){const pal=root.querySelector('.fx-palette');const bottomPal=pal&&pal.offsetWidth>r.width*.6;return {pal,bottomPal,left:bottomPal?8:(pal?pal.offsetWidth+pal.offsetLeft+12:8)};}
  function fit(root,K,d,auto){
    const pos=K.positions(d); const b=kbounds(K,d,pos); const v=view(dkey(K,d));
    const r=root.getBoundingClientRect(); if(r.width<50||r.height<50)return;
    const {pal,bottomPal,left}=palInset(root,r);
    const narrow=r.width<600, pad=narrow?16:32;
    const padL=bottomPal?pad:left+12, padT=topInset(root)+(narrow?16:28), padB=(bottomPal?pal.offsetHeight+20:0)+pad;
    const padR=pad+(F.miniOff||narrow?0:196);
    const aw=Math.max(120,r.width-padL-padR), ah=Math.max(120,r.height-padT-padB);
    const bw=b.x1-b.x0+40, bh=b.y1-b.y0+40;
    const floor=window.innerWidth>=1280?(auto?1:.6):narrow?Math.max(.75,Math.min(1,aw/(NW+40))):.75;
    let k=Math.min(aw/bw,ah/bh,1); k=Math.max(floor,k);
    v.k=k; v.x=padL+(aw-(b.x1-b.x0)*k)/2-b.x0*k;
    if((b.x1-b.x0)*k>aw){const s=(K.starts?K.starts(d):[])[0];const sp=s&&pos[s];v.x=Math.min(padL-b.x0*k,sp?padL+aw/2-(sp.x+NW/2)*k:1e9);}
    v.y=(bh*k>ah)?padT-b.y0*k:padT+(ah-bh*k)/2-b.y0*k;
    applyView(root,K,d);
  }
  function topInset(root){const t=root.querySelector('.fx-tools');return t?t.offsetTop+t.offsetHeight:48;}
  function syncChrome(root){
    const tools=root.querySelector('.fx-tools');
    if(tools){root.classList.remove('tb-compact','tb-tight');const over=()=>tools.scrollWidth>tools.clientWidth+1;
      if(over()){root.classList.add('tb-compact');if(over())root.classList.add('tb-tight');}}
    root.style.setProperty('--fx-top',topInset(root)+'px');
  }
  function focusNode(root,K,d,id,center){
    const pos=K.positions(d);const p=pos[id]; if(!p)return; const v=view(dkey(K,d)); const r=root.getBoundingClientRect();
    const n=K.nodes(d).find(x=>x.id===id);const h=n?hOf(K,d,n):100;
    const sx=p.x*v.k+v.x, sy=p.y*v.k+v.y, ex=sx+NW*v.k, ey=sy+h*v.k;
    const {pal,bottomPal,left}=palInset(root,r); const bottom=r.height-(bottomPal?pal.offsetHeight+20:12);
    if(center&&v.k<.8)v.k=.85;
    const top=topInset(root)+8;
    if(!center&&sx>=left&&ex<=r.width-8&&sy>=top&&ey<=bottom)return;
    v.auto=false;
    if(center){v.x=left+(r.width-left)/2-(p.x+NW/2)*v.k; v.y=top+(bottom-top)/2-(p.y+h/2)*v.k;}
    else{const pad=16;
      if(ey>bottom)v.y-=Math.min(ey-bottom+pad,sy-top); else if(sy<top)v.y+=top-sy+pad;
      if(ex>r.width-8)v.x-=Math.min(ex-(r.width-8)+pad,sx-left); else if(sx<left)v.x+=left-sx+pad;}
    applyView(root,K,d);
  }
  function zoomAt(root,K,d,k,cx,cy){
    const v=view(dkey(K,d)); v.auto=false; k=Math.max(.2,Math.min(2.5,k));
    const wx=(cx-v.x)/v.k, wy=(cy-v.y)/v.k; v.k=k; v.x=cx-wx*k; v.y=cy-wy*k; applyView(root,K,d);
  }
  function minimap(root,K,d,pos){
    const mm=root.querySelector('.fx-mini svg'); if(!mm||F.miniOff)return;
    pos=pos||K.positions(d); const b=kbounds(K,d,pos); const v=view(dkey(K,d));
    const r=root.getBoundingClientRect(); const vx0=-v.x/v.k,vy0=-v.y/v.k,vx1=vx0+r.width/v.k,vy1=vy0+r.height/v.k;
    const x0=Math.min(b.x0,vx0)-40,y0=Math.min(b.y0,vy0)-40,x1=Math.max(b.x1,vx1)+40,y1=Math.max(b.y1,vy1)+40;
    const W=180,H=120,s=Math.min(W/(x1-x0),H/(y1-y0));
    root._mini={x0,y0,s};
    mm.setAttribute('viewBox',`0 0 ${W} ${H}`);
    mm.innerHTML=K.nodes(d).map(n=>{const p=pos[n.id];if(!p)return '';return `<rect class="k-${n.type}" x="${(p.x-x0)*s}" y="${(p.y-y0)*s}" width="${NW*s}" height="${Math.max(2,hOf(K,d,n)*s)}" rx="1.5"/>`;}).join('')
      +`<rect class="vp" x="${(vx0-x0)*s}" y="${(vy0-y0)*s}" width="${(vx1-vx0)*s}" height="${(vy1-vy0)*s}"/>`;
  }

  /* ---------- selection ---------- */
  function select(K,d,id,additive){
    const key=dkey(K,d);const ms=multiSet(key);
    if(additive){if(!id)return;const cur=K.getSel(d);if(cur&&cur!==id)ms.add(cur);
      if(ms.has(id)){ms.delete(id);if(K.getSel(d)===id)K.setSel(d,[...ms].pop()||null);}else{ms.add(id);K.setSel(d,id);}
      if(ms.size===1&&ms.has(K.getSel(d)))ms.clear();}
    else{ms.clear();K.setSel(d,id||null);}
    F._edgeSel=null; App.render(); refocus(key); if(K.onSelected)K.onSelected(d);
  }
  function revealInspector(){if(window.innerWidth>860)return;const i=document.querySelector('.insp.has-node');if(!i)return;
    const r=i.getBoundingClientRect();if(getComputedStyle(i).position!=='fixed'&&(r.top>window.innerHeight-80||r.bottom<0))i.scrollIntoView({block:'nearest',behavior:'smooth'});}
  F.revealInspector=revealInspector;
  function selectedIds(K,d){const key=dkey(K,d);const ids=new Set(multiSet(key));const s=K.getSel(d);if(s)ids.add(s);const all=new Set(K.nodes(d).map(n=>n.id));return [...ids].filter(id=>all.has(id));}
  function refocus(key){requestAnimationFrame(()=>{const r=rootOfKey(key);if(r)r.focus({preventScroll:true});});}
  /* one committed edit: history snapshot happens on re-render (mount → record) */
  function commit(K,d,res,msg){
    if(typeof res==='string'&&res!=='moved'){UI.toast(res);rerenderKey(dkey(K,d));return false;}
    S.save();App.render();refocus(dkey(K,d));
    if(res==='moved')UI.toast('Next step changed',()=>F.undoKey(dkey(K,d)));else if(msg)UI.toast(msg,()=>F.undoKey(dkey(K,d)));
    return true;}
  function rerenderKey(key){const r=rootOfKey(key);if(r)rerender(r);}
  F.undoKey=key=>{const r=rootOfKey(key);const c=r&&ctxOf(r);if(c)histStep(c.K,c.d,-1);};
  function menuAt(x,y,items){setTimeout(()=>{const a=document.createElement('div');a.style.cssText=`position:fixed;left:${x}px;top:${y}px;width:0;height:0`;document.body.appendChild(a);
    UI.menu(a,items);a.remove();const m=document.getElementById('menu');if(m){m.style.left=Math.max(8,Math.min(x,innerWidth-m.offsetWidth-8))+'px';const t=y+4;m.style.top=(t+m.offsetHeight>innerHeight-8?Math.max(8,y-m.offsetHeight-4):t)+'px';}},0);}
  const typeItems=(K,run)=>K.palette.map(t=>({label:K.typeLabel(t),icon:K.typeIcon(t),run:()=>run(t)}));

  /* ---------- interactions (delegated) ---------- */
  let drag=null, spaceDown=false;
  const local=(root,e)=>{const r=root.getBoundingClientRect();return {x:e.clientX-r.left,y:e.clientY-r.top};};
  const world=(root,key,e)=>{const p=local(root,e),v=view(key);return {x:(p.x-v.x)/v.k,y:(p.y-v.y)/v.k};};
  function capture(root,e){try{root.setPointerCapture(e.pointerId);}catch(_){}}
  function rectOf(K,d,pos,id){const n=K.nodes(d).find(x=>x.id===id);const p=pos[id];return n&&p?{id,x:p.x,y:p.y,w:NW,h:hOf(K,d,n)}:null;}
  const sidePt=(r,s)=>s==='t'?[r.x+r.w/2,r.y]:s==='b'?[r.x+r.w/2,r.y+r.h]:s==='l'?[r.x,r.y+r.h/2]:[r.x+r.w,r.y+r.h/2];
  /* magnetic target: pointer inside a node or within 24px (screen) of it */
  function snapTarget(K,d,pos,w,k,exclude){
    const m=24/k;let best=null;
    K.nodes(d).forEach(n=>{if(exclude&&exclude.includes(n.id))return;const r=rectOf(K,d,pos,n.id);if(!r)return;
      const dx=Math.max(r.x-w.x,0,w.x-(r.x+r.w)),dy=Math.max(r.y-w.y,0,w.y-(r.y+r.h));const dist=Math.hypot(dx,dy);
      if(dist<=m&&(!best||dist<best.dist)){const sides=['t','r','b','l'].map(s=>({s,p:sidePt(r,s)})).sort((a,b)=>Math.hypot(a.p[0]-w.x,a.p[1]-w.y)-Math.hypot(b.p[0]-w.x,b.p[1]-w.y));
        best={id:n.id,dist,side:sides[0].s,pt:sides[0].p};}});
    return best;}
  function hiliteTarget(root,t){root.querySelectorAll('.fx-node.drop,.fx-anc.hot').forEach(x=>x.classList.remove('drop','hot'));
    if(!t)return;const el=root.querySelector(`.fx-node[data-node="${CSS.escape(t.id)}"]`);if(el){el.classList.add('drop');const a=el.querySelector(`.fx-anc[data-anc="${t.side}"]`);if(a)a.classList.add('hot');}}
  function preview(root,a,b){const p=root.querySelector('.fx-preview path');if(!a){p.setAttribute('d','');return;}
    const mx=(a[0]+b[0])/2;p.setAttribute('d',Math.abs(a[1]-b[1])>Math.abs(a[0]-b[0])?ortho([a,[a[0],(a[1]+b[1])/2],[b[0],(a[1]+b[1])/2],b]):ortho([a,[mx,a[1]],[mx,b[1]],b]));}
  function cancelDrag(){if(!drag)return;const d0=drag;drag=null;const {root}=d0;
    root.classList.remove('panning','dragging','connecting');hiliteTarget(root,null);preview(root,null);
    const mq=root.querySelector('.fx-marq');if(mq)mq.hidden=true;if(d0.ghost)d0.ghost.remove();
    if(d0.kind==='node'&&d0.moved||d0.kind==='reconn'||d0.kind==='marq')rerender(root);}

  document.addEventListener('pointerdown',e=>{
    const root=e.target.closest&&e.target.closest('.fx-root'); if(!root)return;
    const c=ctxOf(root); if(!c)return; const {K,d,key}=c;
    if(e.target.closest('.fx-tools,.fx-help,.fx-issues,.fx-plus,.fx-link,.fx-lbl-in'))return;
    if(e.button===2)return;
    root.focus({preventScroll:true});
    const mini=e.target.closest('.fx-mini');
    if(mini){e.preventDefault();drag={kind:'mini',root,K,d,key};miniJump(root,K,d,e);capture(root,e);return;}
    const pal=e.target.closest('[data-pal]');
    if(pal){e.preventDefault();drag={kind:'pal',root,K,d,key,type:pal.dataset.pal,sx:e.clientX,sy:e.clientY,moved:false,ghost:null};capture(root,e);return;}
    if(e.target.closest('.fx-palette'))return;
    const pan=e.button===1||spaceDown;
    e.preventDefault();
    if(!pan){
      const pos=K.positions(d);
      let eh=e.target.closest('[data-eh]');
      let edgeUnder=null;
      if(eh&&!eh.closest('.fx-ehs.on')){const under=document.elementsFromPoint(e.clientX,e.clientY).find(x=>x.classList&&x.classList.contains('fx-anc'));if(under){edgeUnder=eh.dataset.edge;eh=null;}}
      if(eh){const ek=eh.dataset.edge;const E=route(K,d,pos,view(key).k).find(x=>x.key===ek);if(!E)return;
        drag={kind:'reconn',root,K,d,key,edge:ek,end:eh.dataset.eh,fixed:eh.dataset.eh==='t'?E.sp:E.tp,fixedId:eh.dataset.eh==='t'?E.from:E.to,origId:eh.dataset.eh==='t'?E.to:E.from,pos,sx:e.clientX,sy:e.clientY,moved:false};
        const g=root.querySelector(`[data-eg="${CSS.escape(ek)}"]`);if(g)g.classList.add('ghost');root.classList.add('connecting');capture(root,e);return;}
      const under=eh?null:document.elementsFromPoint(e.clientX,e.clientY).find(x=>x.classList&&x.classList.contains('fx-anc'));
      const anc=e.target.closest('.fx-anc')||under;const nodeEl=anc?anc.closest('.fx-node'):e.target.closest('.fx-node');
      if(anc&&nodeEl){const id=nodeEl.dataset.node;const r=rectOf(K,d,pos,id);
        drag={kind:'wire',root,K,d,key,from:id,start:sidePt(r,anc.dataset.anc),pos,sx:e.clientX,sy:e.clientY,moved:false,edgeUnder};
        root.classList.add('connecting');capture(root,e);return;}
      if(nodeEl){
        const id=nodeEl.dataset.node; const ms=multiSet(key);
        let ids=(ms.has(id)||K.getSel(d)===id)&&ms.size?selectedIds(K,d):[id];
        if(!ids.includes(id))ids=[id];
        drag={kind:'node',root,K,d,key,id,ids,pos,start:world(root,key,e),orig:Object.fromEntries(ids.map(i=>[i,{...pos[i]}])),moved:false,shift:e.shiftKey};
        capture(root,e); return;
      }
      const groupEl=e.target.closest('[data-group]');
      if(groupEl&&K.groups){const g=groupEl.dataset.group;
        const ids=K.nodes(d).filter(n=>(g[0]==='p'?n.page:n.lane)===g.slice(2)).map(n=>n.id);
        drag={kind:'node',root,K,d,key,id:null,ids,pos,start:world(root,key,e),orig:Object.fromEntries(ids.map(i=>[i,{...pos[i]}])),moved:false,group:true};
        capture(root,e); return;}
      const edgeEl=e.target.closest('[data-edge]');
      if(edgeEl){drag={kind:'edgeclick',root,K,d,key,edge:edgeEl.dataset.edge,cx:e.clientX,cy:e.clientY,onLabel:!!e.target.closest('.fx-elabel')};return;}
      if(e.shiftKey||F.tool==='select'){const p=local(root,e);
        drag={kind:'marq',root,K,d,key,p0:p,add:e.shiftKey,base:e.shiftKey?selectedIds(K,d):[],pos};capture(root,e);return;}
    }
    drag={kind:'pan',root,K,d,key,sx:e.clientX,sy:e.clientY,vx:view(key).x,vy:view(key).y,moved:false};
    root.classList.add('panning'); capture(root,e);
  });

  function hotPlus(e){
    const root=e.target.closest&&e.target.closest('.fx-root');
    const prev=document.querySelectorAll('.fx-plus.show');
    if(!root){prev.forEach(x=>x.classList.remove('show'));return;}
    let keys=[];const edge=e.target.closest('[data-edge]'),pl=e.target.closest('[data-plus]'),node=e.target.closest('.fx-node');
    if(edge)keys=[edge.dataset.edge];else if(pl)keys=[pl.dataset.plus];
    else if(node){keys=[...root.querySelectorAll('[data-eg]')].map(g=>g.dataset.eg).filter(k=>k.slice(0,k.lastIndexOf(':'))===node.dataset.node);}
    const c=ctxOf(root);const sel=c&&edgeSelKey(c.K,c.d);
    prev.forEach(x=>{if(!keys.includes(x.dataset.plus)&&x.dataset.plus!==sel)x.classList.remove('show');});
    keys.forEach(k=>{const b=root.querySelector(`.fx-plus[data-plus="${CSS.escape(k)}"]`);if(b)b.classList.add('show');});
    root.querySelectorAll('.fx-ehs.hov').forEach(x=>{if(!keys.includes(x.dataset.ehs))x.classList.remove('hov');});
    if(node&&!edge&&!pl)keys=[];
    keys.forEach(k=>{const h=root.querySelector(`.fx-ehs[data-ehs="${CSS.escape(k)}"]`);if(h)h.classList.add('hov');});
  }
  let moveEv=null,moveRaf=0;
  document.addEventListener('pointermove',e=>{
    if(!drag){if(e.pointerType==='mouse')hotPlus(e);return;}
    moveEv=e; if(drag.kind==='node'||drag.kind==='pan'){if(!moveRaf)moveRaf=requestAnimationFrame(()=>{moveRaf=0;if(drag&&moveEv)onMove(moveEv);});return;}
    onMove(e);
  });
  function onMove(e){
    const {root,K,d,key}=drag;
    if(drag.kind==='pan'){const v=view(key);v.auto=false;if(Math.abs(e.clientX-drag.sx)+Math.abs(e.clientY-drag.sy)>3)drag.moved=true;v.x=drag.vx+e.clientX-drag.sx;v.y=drag.vy+e.clientY-drag.sy;applyView(root,K,d);return;}
    if(drag.kind==='mini'){miniJump(root,K,d,e);return;}
    if(drag.kind==='edgeclick')return;
    if(drag.kind==='wire'||drag.kind==='reconn'){
      if(Math.abs(e.clientX-drag.sx)+Math.abs(e.clientY-drag.sy)>4)drag.moved=true;
      const w=world(root,key,e);const excl=drag.kind==='wire'?[drag.from]:[];
      const t=snapTarget(K,d,drag.pos,w,view(key).k,excl);drag.target=t;hiliteTarget(root,t);
      const a=drag.kind==='wire'?drag.start:drag.fixed;const b=t?t.pt:[w.x,w.y];
      if(drag.kind==='reconn'&&drag.end==='s')preview(root,b,a);else preview(root,a,b);return;}
    if(drag.kind==='marq'){const p=local(root,e);const mq=root.querySelector('.fx-marq');
      const x=Math.min(p.x,drag.p0.x),y=Math.min(p.y,drag.p0.y),w=Math.abs(p.x-drag.p0.x),h=Math.abs(p.y-drag.p0.y);
      if(w+h<4)return;drag.moved=true;mq.hidden=false;Object.assign(mq.style,{left:x+'px',top:y+'px',width:w+'px',height:h+'px'});
      const v=view(key);const wr={x:(x-v.x)/v.k,y:(y-v.y)/v.k,w:w/v.k,h:h/v.k};
      const hit=K.nodes(d).map(n=>rectOf(K,d,drag.pos,n.id)).filter(r=>r&&inter(r,wr)).map(r=>r.id);drag.hit=hit;
      root.querySelectorAll('.fx-node').forEach(el=>el.classList.toggle('sel',hit.includes(el.dataset.node)||drag.base.includes(el.dataset.node)));return;}
    if(drag.kind==='pal'){
      if(!drag.moved&&Math.abs(e.clientX-drag.sx)+Math.abs(e.clientY-drag.sy)>5){drag.moved=true;
        const g=document.createElement('div');g.className='fx-ghost k-'+drag.type;g.innerHTML=`${ic(K.typeIcon(drag.type),'',14)}<b>${K.typeLabel(drag.type)}</b>`;document.body.appendChild(g);drag.ghost=g;}
      if(drag.ghost){drag.ghost.style.transform=`translate(${e.clientX+8}px,${e.clientY+8}px)`;
        root.querySelectorAll('.fx-node.drop').forEach(x=>x.classList.remove('drop'));
        const over=document.elementFromPoint(e.clientX,e.clientY); const t=over&&over.closest('.fx-node'); if(t&&root.contains(t))t.classList.add('drop');}
      return;}
    if(drag.kind==='node'){
      const w=world(root,key,e); const dx=w.x-drag.start.x, dy=w.y-drag.start.y;
      if(!drag.moved&&Math.hypot(dx,dy)*view(key).k<4)return;
      if(!drag.moved){drag.moved=true;root.classList.add('dragging');}
      drag.ids.forEach(id=>{const o=drag.orig[id];if(!o)return;const x=e.altKey?o.x+dx:snap(o.x+dx),y=e.altKey?o.y+dy:snap(o.y+dy);drag.pos[id]={x,y};
        const el=root.querySelector(`.fx-node[data-node="${CSS.escape(id)}"]`);if(el){el.style.transform=`translate(${x}px,${y}px)`;el.classList.add('lift');}});
      redraw(root,K,d,drag.pos,true);
    }
  }
  function miniJump(root,K,d,e){
    const mm=root.querySelector('.fx-mini svg'); const info=root._mini; if(!mm||!info)return;
    const r=mm.getBoundingClientRect(); const sc=180/r.width;
    const wx=(e.clientX-r.left)*sc/info.s+info.x0, wy=(e.clientY-r.top)*sc/info.s+info.y0;
    const rr=root.getBoundingClientRect(); const v=view(dkey(K,d)); v.x=rr.width/2-wx*v.k; v.y=rr.height/2-wy*v.k; applyView(root,K,d);
  }
  /* create a node where a dangling connection was dropped */
  function dropCreate(K,d,w,cx,cy,after){
    menuAt(cx,cy,typeItems(K,t=>{S.snap('Add '+K.noun);const id=K.addAt(d,t,w.x,w.y);const res=after(id);
      if(typeof res==='string'){UI.toast(res);}K.setSel(d,id);multiSet(dkey(K,d)).clear();S.save();App.render();refocus(dkey(K,d));}));
  }
  function doConnect(K,d,from,to,cx,cy){
    if(K.askEdge){K.askEdge(d,from,to,cx,cy,opt=>{S.snap('Connect');commit(K,d,K.connect(d,from,to,opt));});rerenderKey(dkey(K,d));return;}
    S.snap('Connect');commit(K,d,K.connect(d,from,to));
  }
  document.addEventListener('pointerup',e=>{
    if(!drag)return; if(moveRaf){cancelAnimationFrame(moveRaf);moveRaf=0;if(moveEv&&(drag.kind==='node'))onMove(moveEv);}
    const g=drag; drag=null; const {root,K,d,key}=g;
    root.classList.remove('panning','dragging','connecting');hiliteTarget(root,null);preview(root,null);
    if(g.kind==='pan'){if(!g.moved&&(K.getSel(d)||multiSet(key).size||F._edgeSel)){F._edgeSel=null;select(K,d,null);}return;}
    if(g.kind==='mini')return;
    if(g.kind==='edgeclick'){F._edgeSel={doc:key,key:g.edge};
      if(K.getSel(d)||multiSet(key).size){K.setSel(d,null);multiSet(key).clear();App.render();refocus(key);}else redraw(root,K,d);
      if(K.editEdgeOnClick&&g.onLabel)K.editEdge(d,g.edge,g.cx,g.cy,()=>{S.save();App.render();refocus(key);});
      return;}
    if(g.kind==='marq'){const mq=root.querySelector('.fx-marq');mq.hidden=true;
      if(!g.moved){if(K.getSel(d)||multiSet(key).size||F._edgeSel){F._edgeSel=null;select(K,d,null);}return;}
      const ids=[...new Set(g.base.concat(g.hit||[]))];const ms=multiSet(key);ms.clear();
      if(ids.length>1)ids.forEach(i=>ms.add(i));K.setSel(d,ids[ids.length-1]||null);F._edgeSel=null;App.render();refocus(key);return;}
    if(g.kind==='wire'){
      const w=world(root,key,e);
      if(g.target){doConnect(K,d,g.from,g.target.id,e.clientX,e.clientY);return;}
      if(!g.moved&&g.edgeUnder){F._edgeSel={doc:key,key:g.edgeUnder};if(K.getSel(d)||multiSet(key).size){K.setSel(d,null);multiSet(key).clear();App.render();refocus(key);}else redraw(root,K,d);return;}
      if(g.moved)dropCreate(K,d,w,e.clientX,e.clientY,id=>K.askEdge?K.connect(d,g.from,id,K.defaultEdge):K.connect(d,g.from,id));
      return;}
    if(g.kind==='reconn'){
      const w=world(root,key,e);
      if(g.target&&g.target.id!==g.origId){S.snap('Reconnect');commit(K,d,K.reconnect(d,g.edge,g.end,g.target.id),null);F._edgeSel=null;return;}
      if(!g.target&&g.moved){rerender(root);dropCreate(K,d,w,e.clientX,e.clientY,id=>K.reconnect(d,g.edge,g.end,id));return;}
      if(!g.moved){F._edgeSel={doc:key,key:g.edge};if(K.getSel(d)||multiSet(key).size){K.setSel(d,null);multiSet(key).clear();App.render();refocus(key);return;}}
      rerender(root);return;}
    if(g.kind==='pal'){
      if(g.ghost)g.ghost.remove();
      const r=root.getBoundingClientRect(),v=view(key);
      if(!g.moved){S.snap('Add '+K.noun);const id=K.paletteClick(d,g.type,{x:(r.width/2-v.x)/v.k,y:(r.height/2-v.y)/v.k});
        if(id){S.save();multiSet(key).clear();K.setSel(d,id);F._focus[key]=id;App.render();refocus(key);if(K.onSelected)K.onSelected(d);}return;}
      if(e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom)return;
      const over=document.elementFromPoint(e.clientX,e.clientY);if(over&&over.closest('.fx-palette'))return;
      const t=over&&over.closest('.fx-node');S.snap('Add '+K.noun);
      const id=K.paletteDrop(d,g.type,world(root,key,e),t&&root.contains(t)?t.dataset.node:null);
      S.save();multiSet(key).clear();K.setSel(d,id);F._focus[key]=id;App.render();refocus(key);return;}
    if(g.kind==='node'){
      if(g.moved){root.querySelectorAll('.fx-node.lift').forEach(x=>x.classList.remove('lift'));S.snap('Move');persist(K,d,g.pos);rerender(root);return;}
      if(g.group)return;
      if(g.shift){select(K,d,g.id,true);return;}
      if(K.getSel(d)!==g.id||multiSet(key).size||F._edgeSel)select(K,d,g.id,false);
    }
  });
  document.addEventListener('pointercancel',()=>cancelDrag());

  document.addEventListener('dblclick',e=>{
    const root=e.target.closest&&e.target.closest('.fx-root');if(!root)return;const c=ctxOf(root);if(!c)return;
    const el=e.target.closest('[data-edge]');if(!el)return;e.preventDefault();
    c.K.editEdge&&c.K.editEdge(c.d,el.dataset.edge,e.clientX,e.clientY,()=>{S.save();App.render();refocus(c.key);},root);
  });
  /* inline label editor for SOP branches */
  KINDS.sop.editEdge=function(sop,key,cx,cy,done,root){
    root=root||document.querySelector(`.fx-root[data-kind="sop"][data-flow="${CSS.escape(sop.id)}"]`);if(!root)return;
    const old=KINDS.sop.edgeText(sop,key);const r=root.getBoundingClientRect();
    const inp=document.createElement('input');inp.className='fx-lbl-in';inp.value=old;inp.setAttribute('aria-label','Branch label');inp.placeholder='Label';
    inp.style.left=(cx-r.left-60)+'px';inp.style.top=(cy-r.top-16)+'px';root.appendChild(inp);inp.focus();inp.select();
    let closed=false;const fin=ok=>{if(closed)return;closed=true;const val=inp.value.trim();inp.remove();
      if(ok&&val!==old){S.snap('Rename branch');KINDS.sop.setEdgeText(sop,key,val);done();}else refocus('sop:'+sop.id);};
    inp.addEventListener('keydown',ev=>{ev.stopPropagation();if(ev.key==='Enter')fin(true);if(ev.key==='Escape')fin(false);});
    inp.addEventListener('blur',()=>fin(true));
  };

  document.addEventListener('contextmenu',e=>{
    const root=e.target.closest&&e.target.closest('.fx-root');if(!root||e.target.closest('.fx-tools,.fx-palette,.fx-mini'))return;
    const c=ctxOf(root);if(!c)return;const {K,d,key}=c;e.preventDefault();cancelDrag();
    const nodeEl=e.target.closest('.fx-node'),edgeEl=e.target.closest('[data-edge]');
    const done=msg=>{S.save();App.render();refocus(key);if(msg)UI.toast(msg,()=>F.undoKey(key));};
    if(nodeEl){const id=nodeEl.dataset.node;let ids=selectedIds(K,d);
      if(!ids.includes(id)){multiSet(key).clear();K.setSel(d,id);ids=[id];App.render();}
      const items=[];
      if(ids.length===1&&(!K.canAddAfter||K.canAddAfter(d,id)))items.push({label:'Add '+K.noun+' after',icon:'plus',run:()=>menuAt(e.clientX,e.clientY,typeItems(K,t=>{S.snap('Add');const nid=K.addAfter(d,t,id);if(nid){K.setSel(d,nid);multiSet(key).clear();F._focus[key]=nid;}done();}))});
      items.push({label:'Duplicate',icon:'copy',run:()=>{S.snap('Duplicate');const out=K.duplicate(d,ids);const ms=multiSet(key);ms.clear();if(out.length>1)out.forEach(x=>ms.add(x));if(out.length)K.setSel(d,out[out.length-1]);done();}});
      items.push({label:'Disconnect',icon:'split',run:()=>{S.snap('Disconnect');K.disconnect(d,ids);done('Connections removed');}});
      items.push('-',{label:ids.length>1?'Delete '+ids.length+' '+K.noun+'s':'Delete',icon:'trash',danger:true,run:()=>deleteNodes(K,d,ids)});
      menuAt(e.clientX,e.clientY,items);return;}
    if(edgeEl){const ek=edgeEl.dataset.edge;F._edgeSel={doc:key,key:ek};redraw(root,K,d);
      menuAt(e.clientX,e.clientY,[{label:K.edgeEditLabel||'Rename label',icon:'edit-3',run:()=>K.editEdge(d,ek,e.clientX,e.clientY,()=>done(),root)},
        '-',{label:'Delete connection',icon:'trash',danger:true,run:()=>{S.snap('Delete connection');K.deleteEdge(d,ek);F._edgeSel=null;done('Connection removed');}}]);return;}
    const w=world(root,key,e);
    menuAt(e.clientX,e.clientY,typeItems(K,t=>{S.snap('Add');const id=K.addAt(d,t,w.x,w.y);K.setSel(d,id);multiSet(key).clear();done();}).map(x=>Object.assign(x,{label:'Add '+x.label.toLowerCase()+' here'})));
  });
  function deleteNodes(K,d,ids){const key=dkey(K,d);
    S.snap('Delete');const n=K.removeNodes(d,ids);
    if(!n){UI.toast('This '+K.noun+' cannot be deleted');return;}
    S.save();multiSet(key).clear();K.setSel(d,null);F._edgeSel=null;App.render();refocus(key);
    UI.toast(n>1?n+' '+K.noun+'s deleted':(K.noun[0].toUpperCase()+K.noun.slice(1))+' deleted',()=>F.undoKey(key));}

  document.addEventListener('wheel',e=>{
    const root=e.target.closest&&e.target.closest('.fx-root'); if(!root||e.target.closest('.fx-palette,.fx-help,.fx-issues'))return;
    const c=ctxOf(root); if(!c)return; e.preventDefault();
    const v=view(c.key);
    const m=e.deltaMode===1?16:1; const dx=e.deltaX*m, dy=e.deltaY*m;
    if(e.ctrlKey||e.metaKey){const p=local(root,e);zoomAt(root,c.K,c.d,v.k*Math.exp(-dy*(e.ctrlKey?0.01:0.0022)),p.x,p.y);return;}
    v.auto=false;
    if(e.shiftKey&&!dx){v.x-=dy;}else{v.x-=dx;v.y-=dy;}
    applyView(root,c.K,c.d);
  },{passive:false});

  document.addEventListener('change',e=>{
    const j=e.target.closest&&e.target.closest('.fx-root [data-fxjump]'); if(!j||!j.value)return;
    const root=j.closest('.fx-root'),c=ctxOf(root); if(!c)return;const {K,d}=c;
    const pos=K.positions(d); const first=K.nodes(d).filter(n=>n.page===j.value&&pos[n.id]).sort((a,b)=>pos[a.id].y-pos[b.id].y)[0];
    if(first){const v=view(c.key),r=root.getBoundingClientRect();if(v.k<.8)v.k=.85;v.auto=false;const {left}=palInset(root,r);
      v.x=left+(r.width-left)/2-(pos[first.id].x+NW/2)*v.k; v.y=topInset(root)+24-(pos[first.id].y-66)*v.k; applyView(root,K,d);}
    j.value='';
  });
  document.addEventListener('click',e=>{
    const iss=e.target.closest&&e.target.closest('.fx-root [data-fxissue]');
    if(iss){const root=iss.closest('.fx-root'),c=ctxOf(root);const id=iss.dataset.fxissue;
      if(id&&c.K.nodes(c.d).some(n=>n.id===id)){multiSet(c.key).clear();c.K.setSel(c.d,id);F._focus[c.key]={id,center:true};App.render();refocus(c.key);}
      else root.querySelector('.fx-issues').hidden=true;
      return;}
    const plus=e.target.closest&&e.target.closest('.fx-root [data-plus]');
    if(plus){e.stopPropagation();const root=plus.closest('.fx-root'),c=ctxOf(root);if(!c)return;
      setTimeout(()=>UI.menu(plus,c.K.palette.filter(t=>t!=='end').map(t=>({label:c.K.typeLabel(t),icon:c.K.typeIcon(t),run(){
        S.snap('Insert');const id=c.K.insertOnEdge(c.d,plus.dataset.plus,t); if(!id)return; S.save(); multiSet(c.key).clear(); c.K.setSel(c.d,id); F._focus[c.key]=id; App.render(); refocus(c.key);}}))),0);
      return;}
    const b=e.target.closest&&e.target.closest('.fx-root [data-fx]'); if(!b)return;
    const root=b.closest('.fx-root'),c=ctxOf(root); if(!c)return; const {K,d,key}=c;
    const r=root.getBoundingClientRect(),v=view(key),fx=b.dataset.fx;
    if(fx==='tool-select'||fx==='tool-pan'){F.tool=fx==='tool-select'?'select':'pan';document.querySelectorAll('.fx-root').forEach(x=>{x.classList.toggle('tool-select',F.tool==='select');x.classList.toggle('tool-pan',F.tool==='pan');
      x.querySelector('[data-fx=tool-select]').classList.toggle('on',F.tool==='select');x.querySelector('[data-fx=tool-pan]').classList.toggle('on',F.tool==='pan');});}
    if(fx==='in')zoomAt(root,K,d,v.k*1.2,r.width/2,r.height/2);
    if(fx==='out')zoomAt(root,K,d,v.k/1.2,r.width/2,r.height/2);
    if(fx==='fit')fit(root,K,d);
    if(fx==='tidy'){S.snap('Tidy layout');delete d.layout;S.save();v.fresh=true;App.render();UI.toast('Re-arranged',()=>F.undoKey(key));}
    if(fx==='undo')histStep(K,d,-1);
    if(fx==='redo')histStep(K,d,1);
    if(fx==='validate'){const box=root.querySelector('.fx-issues');const is=K.validate(d);
      if(!box.hidden){box.hidden=true;return;}
      box.innerHTML=is.length?`<div class="fx-issues-h">${is.length} issue${is.length>1?'s':''}</div>`+is.map(i=>`<button data-fxissue="${esc(i.id||'')}">${ic('reject','',14)}<span>${esc(i.msg)}</span></button>`).join(''):`<div class="fx-issues-ok">${ic('check','',15)}Ready to publish</div>`;
      box.hidden=false;}
    if(fx==='help'){const h=root.querySelector('.fx-help');h.hidden=!h.hidden;b.setAttribute('aria-expanded',String(!h.hidden));b.classList.toggle('on',!h.hidden);}
    if(fx==='mini'){F.miniOff=!F.miniOff;root.querySelector('.fx-mini').classList.toggle('hidden',F.miniOff);root.querySelectorAll('[data-fx=mini]').forEach(x=>x.classList.toggle('on',!F.miniOff));minimap(root,K,d);}
    if(fx==='pal'){F.palOpen=!F.palOpen;root.classList.toggle('pal-open',F.palOpen);b.setAttribute('aria-expanded',String(F.palOpen));syncChrome(root);if(v.auto)fit(root,K,d);}
    if(fx==='more'){e.stopPropagation();const click=k=>{const t=root.querySelector(`.fx-tools [data-fx="${k}"]`);if(t)t.click();};
      const tight=root.classList.contains('tb-tight');
      setTimeout(()=>UI.menu(b,[].concat(tight?[{label:'Zoom in',icon:'zoomin',run:()=>click('in')},{label:'Zoom out',icon:'zoomout',run:()=>click('out')}]:[],
        [...(innerWidth<=420?[{label:'Redo',icon:'undo',run:()=>click('redo')}]:[]),{label:'Tidy layout',icon:'workflow',run:()=>click('tidy')},{label:(F.miniOff?'Show':'Hide')+' minimap',icon:'layers',run:()=>click('mini')},{label:'Shortcuts',icon:'help',run:()=>click('help')}])),0);}
  });

  const typing=t=>t&&(t.isContentEditable||/^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName));
  document.addEventListener('keydown',e=>{
    if(e.key==='Escape'&&drag){e.preventDefault();e.stopPropagation();cancelDrag();return;}
    if(e.code==='Space'&&!typing(e.target)&&document.querySelector('.fx-root')){if(!spaceDown){spaceDown=true;document.querySelectorAll('.fx-root').forEach(r=>r.classList.add('space'));}if(e.target.closest&&e.target.closest('.fx-root'))e.preventDefault();return;}
    const root=e.target.closest&&e.target.closest('.fx-root'); if(!root||typing(e.target))return;
    const c=ctxOf(root); if(!c)return; const {K,d,key}=c;
    if((e.metaKey||e.ctrlKey)&&(e.key==='z'||e.key==='Z')){e.preventDefault();histStep(K,d,e.shiftKey?1:-1);return;}
    if((e.metaKey||e.ctrlKey)&&e.key==='y'){e.preventDefault();histStep(K,d,1);return;}
    if((e.metaKey||e.ctrlKey)&&(e.key==='a'||e.key==='A')){e.preventDefault();const ms=multiSet(key);ms.clear();const all=K.nodes(d).map(n=>n.id);all.forEach(x=>ms.add(x));K.setSel(d,all[all.length-1]||null);App.render();refocus(key);return;}
    const ids=selectedIds(K,d);
    if(e.key==='Delete'||e.key==='Backspace'){
      e.preventDefault();
      const es=edgeSelKey(K,d);
      if(es){S.snap('Delete connection');K.deleteEdge(d,es);F._edgeSel=null;S.save();App.render();refocus(key);UI.toast('Connection removed',()=>F.undoKey(key));return;}
      if(ids.length)deleteNodes(K,d,ids);
    }else if((e.metaKey||e.ctrlKey)&&(e.key==='d'||e.key==='D')){
      e.preventDefault(); if(!ids.length)return;
      S.snap('Duplicate');const out=K.duplicate(d,ids); if(!out.length)return; S.save();
      const ms=multiSet(key);ms.clear();if(out.length>1)out.forEach(i=>ms.add(i));K.setSel(d,out[out.length-1]);App.render();refocus(key);
    }else if(e.key==='Escape'){if(document.getElementById('menu'))return;multiSet(key).clear();const had=K.getSel(d)||F._edgeSel;F._edgeSel=null;K.setSel(d,null);if(had){App.render();refocus(key);}}
    else if((e.metaKey||e.ctrlKey)&&e.key==='0'){e.preventDefault();fit(root,K,d);}
    else if(!e.metaKey&&!e.ctrlKey&&(e.key==='v'||e.key==='V')){root.querySelector('[data-fx=tool-select]').click();}
    else if(!e.metaKey&&!e.ctrlKey&&(e.key==='h'||e.key==='H')){root.querySelector('[data-fx=tool-pan]').click();}
    else if((e.key==='='||e.key==='+')&&!e.metaKey&&!e.ctrlKey){const r=root.getBoundingClientRect();zoomAt(root,K,d,view(key).k*1.2,r.width/2,r.height/2);}
    else if(e.key==='-'&&!e.metaKey&&!e.ctrlKey){const r=root.getBoundingClientRect();zoomAt(root,K,d,view(key).k/1.2,r.width/2,r.height/2);}
  },true);
  document.addEventListener('keyup',e=>{if(e.code==='Space'){spaceDown=false;document.querySelectorAll('.fx-root').forEach(r=>r.classList.remove('space'));}});
  window.addEventListener('resize',()=>{document.querySelectorAll('.fx-root').forEach(r=>{const c=ctxOf(r);if(c){syncChrome(r);minimap(r,c.K,c.d);}});});
  const resync=()=>document.querySelectorAll('.fx-root').forEach(r=>syncChrome(r));
  window.addEventListener('load',resync); if(document.fonts&&document.fonts.ready)document.fonts.ready.then(resync);
  F.nodeWidth=NW;

  /* ---------- structural edits ---------- */
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
