/* Master SOP composition: stages with prerequisites, parallel lanes, approvals, waits; try run */
(function(){
  const M={sel:{},try:{}};
  const base='#/configuration/work-instructions/master/';
  const STATE_L={started:'started',completed:'completed',approved:'approved'};

  function layout(stages){
    const by={};stages.forEach(s=>by[s.id]=s);
    const rank={},col={};
    const rk=(id,g)=>{if(rank[id]!=null)return rank[id];if((g||0)>40)return 0;const s=by[id];if(!s)return 0;let r=0;
      s.deps.forEach(d=>{if(!by[d.stage])return;const pr=rk(d.stage,(g||0)+1);r=Math.max(r,d.state==='started'?pr:pr+1);});return rank[id]=r;};
    stages.forEach(s=>rk(s.id));
    const rows={};stages.forEach(s=>(rows[rank[s.id]]=rows[rank[s.id]]||[]).push(s.id));
    Object.values(rows).forEach(ids=>ids.forEach((id,i)=>col[id]=i));
    /* estimated card height: header + name + child line + one row per tag (tags wrap); stages keep a >=48px connector gap */
    const hOf=s=>84+26*(s.deps.length+(s.approvalRoleId?1:0)+(s.waitDays||s.waitSetting?1:0)+(s.repeatHours||s.repeatSetting?1:0))+44*((S.get('settings',s.waitSetting)?1:0)+(S.get('settings',s.repeatSetting)?1:0));
    const maxR=Math.max(0,...Object.values(rank)); const rowY=[]; let y=34;
    for(let r=0;r<=maxR;r++){rowY[r]=y;y+=Math.max(112,...stages.filter(s=>rank[s.id]===r).map(hOf))+56;}
    const pos={};let maxc=0;stages.forEach(s=>{pos[s.id]={x:34+col[s.id]*360,y:rowY[rank[s.id]],w:260,h:Math.max(112,hOf(s))};maxc=Math.max(maxc,col[s.id]);});
    return {pos,W:34+(maxc+1)*360,H:y-22};
  }
  function validate(m){
    const iss=[],by={};m.stages.forEach(s=>by[s.id]=s);
    const labels=m.stages.map(s=>REG.norm(s.label));
    m.stages.forEach(s=>{
      if(!s.label)iss.push(s.label+'Stage without a name');
      if(labels.indexOf(REG.norm(s.label))!==labels.lastIndexOf(REG.norm(s.label)))iss.push(s.label+': duplicate name');
      if(!S.get('sops',s.sopId))iss.push(s.label+': choose a child SOP');
      else if(Flow.childLoop&&Flow.childLoop(s.sopId))iss.push(s.label+': child SOP loops back into itself');
      s.deps.forEach(d=>{const t=by[d.stage];if(d.stage===s.id)iss.push(s.label+': cannot wait on itself');else if(!t)iss.push(s.label+': missing prerequisite');else if(d.state==='approved'&&!t.approvalRoleId)iss.push(s.label+': '+t.label+' has no approval');});
    });
    const vis={},stack=[];const loops=new Set();
    const dfs=id=>{const k=stack.indexOf(id);if(k>=0){loops.add(stack.slice(k).map(x=>(by[x]||{}).label).join(' → ')+' → '+(by[id]||{}).label);return;}if(vis[id])return;vis[id]=1;stack.push(id);(by[id]?by[id].deps:[]).forEach(d=>d.stage!==id&&dfs(d.stage));stack.pop();};
    m.stages.forEach(s=>dfs(s.id)); loops.forEach(l=>iss.push('Prerequisites loop: '+l));
    return iss;
  }
  const tr=id=>M.try[id]||(M.try[id]={st:{},cohort:null});
  function stageState(m,s,T){
    const x=T.st[s.id]||{};
    const met=s.deps.every(d=>{const y=T.st[d.stage]||{};return d.state==='started'?y.started:d.state==='completed'?y.completed:y.approved;});
    if(!x.started)return met?['Ready','info']:['Locked','mut'];
    if(!x.completed){if(s.waitDays&&!x.waitDone)return ['Waiting','warn'];return ['In progress','info'];}
    if(s.approvalRoleId&&!x.approved)return ['Awaiting approval','pur'];
    return [s.approvalRoleId?'Approved':'Completed','ok'];
  }
  const val=(s,k,sk)=>{const st=S.get('settings',s[sk]);return st?st.value:s[k];};
  /* published snapshot vs draft; values that follow a setting compare by the setting, not the copied number */
  const norm=stages=>(stages||[]).map(x=>{const y=Object.assign({},x);if(y.waitSetting)delete y.waitDays;if(y.repeatSetting)delete y.repeatHours;
    Object.keys(y).forEach(k=>{if(y[k]===''||y[k]==null)delete y[k];});return y;});
  let seedM=null;
  function snapshot(m,v){if(!v)return null;if(!v.stages){if(!seedM){try{seedM={};(window.buildSeed().masters||[]).forEach(x=>seedM[x.id]=x.stages);}catch(e){seedM={};}}
      v.stages=JSON.parse(JSON.stringify(seedM[m.id]||m.stages));}
    return v.stages;}
  function pending(m){const v=m.versions[m.versions.length-1];if(!v)return {changed:true,drift:[],edits:true};
    const snap=snapshot(m,v);const edits=Flow.stable(norm(snap))!==Flow.stable(norm(m.stages))||(v.title!==undefined&&v.title!==m.title);
    const drift=Flow.drift(m.stages,v);return {changed:edits||drift.length>0,drift,edits};}
  M.pending=pending;

  M.page=function(id,view){
    const m=S.get('masters',id); if(!m)return '<div class="empty">Not found</div>';
    const iss=validate(m); const v=m.versions[m.versions.length-1]; const T=tr(id); const pd=pending(m);
    const canvas=`<div class="canvas" data-canvas>${Flow.kindCanvas('master',m,M.sel[id],{view})}</div>`;
    const head=`<div class="phead"><div><div class="crumb">Configuration / <a href="#/configuration/work-instructions">Work instructions</a> / <b>${esc(m.dept)}</b></div><h1 class="mst-h1">${esc(m.title).replace(/-/g,'\u2011')}</h1>
      <div class="vstrip" style="margin-top:6px">${v?UI.tag('v'+v.v+' published','ok'):UI.tag('Draft','warn')}${v&&pd.edits?UI.tag('Draft changes','warn'):''}${pd.drift.length?UI.tag('Setting changed · publish to apply','warn'):''}${v&&v.running?UI.tag(v.running+' running','info'):''}${UI.tag(m.stages.length+' stages','mut')}${iss.length?UI.tag(iss.length+' issues','dng'):''}</div></div><div class="sp"></div>
      <div class="seg"><a class="${view!=='try'?'on':''}" href="${base}${id}">${ic('layers')}Stages</a><a class="${view==='try'?'on':''}" href="${base}${id}/try">${ic('play')}Try</a></div>
      ${Flow.readOnly()?UI.tag('View only','mut'):`<button class="btn p" data-a="mst-publish" data-id="${id}" ${iss.length||!pd.changed?'disabled':''} title="${pd.changed?'':'No changes since v'+(v?v.v:0)}">${ic('check')}Publish v${v?v.v+1:1}</button>`}</div>`;
    const side=view==='try'?trySide(m,T):inspector(m,iss);
    return head+`<div class="editor"><section class="card"><div class="canvasbar"><div class="fx-legend"><span><i></i>After completed</span><span><i class="a"></i>After approved</span><span><i class="s"></i>Parallel (after started)</span></div></div>${canvas}</section><section class="card insp ${M.sel[id]&&m.stages.some(x=>x.id===M.sel[id])?'has-node':''}">${side}</section></div>`;
  };

  function inspector(m,iss){
    const s=m.stages.find(x=>x.id===M.sel[m.id]);
    if(!s)return `<div class="hd"><h3>Master SOP</h3></div><div class="bd">
      <div class="fld full"><label>Title</label><textarea data-ms="title" rows="2" class="mst-title">${esc(m.title)}</textarea></div>
      ${(()=>{const pd=pending(m);return pd.drift.length?`<div class="eyebrow mt">Pending setting changes</div><div class="stack" style="gap:4px;margin-top:6px">${pd.drift.map(x=>`<div class="fx-set moved">Uses setting: ${esc(x.name)} = ${esc(x.now)}${x.unit?' '+esc(x.unit):''} <b>(was ${esc(x.was)})</b></div>`).join('')}<div class="small muted">Running work keeps the old value. Publish to use the new one.</div></div>`:'';})()}
      <div class="eyebrow mt">Issues</div>${iss.length?iss.map(i=>`<div class="small" style="color:var(--danger);margin-top:6px">${esc(i)}</div>`).join(''):`<div class="mt">${UI.tag('Ready to publish','ok')}</div>`}</div>`;
    const others=m.stages.filter(x=>x.id!==s.id);
    return `<div class="hd">${ic('layers')}<h3>Stage</h3><span class="sp"></span><button class="btn sm gh" data-a="stage-sel" data-id="">${ic('x')}</button></div><div class="bd">
      <div class="fld"><label>Name</label><input data-st="label" value="${esc(s.label)}"></div>
      <div class="fld"><label>Child SOP</label><select data-st="sopId"><option value="">—</option>${S.active('sops').map(x=>`<option value="${x.id}" ${s.sopId===x.id?'selected':''}>${esc(x.dept)} · ${esc(x.title)} (${Flow.stepCount(x.nodes)})</option>`).join('')}<option value="__new">+ Create “${esc(s.label)}” SOP</option></select></div>
      ${S.get('sops',s.sopId)?`<a class="btn sm mb" href="#/configuration/work-instructions/${s.sopId}">${ic('workflow')}Open flowchart</a>`:''}
      <div class="eyebrow mt">Prerequisites</div>
      ${s.deps.map((d,i)=>`<div class="row nw mt"><select class="inl" style="flex:1.4" data-dep="${i}" data-k="stage">${others.map(o=>`<option value="${o.id}" ${d.stage===o.id?'selected':''}>${esc(o.label)}</option>`).join('')}</select>
        <select class="inl" style="flex:1" data-dep="${i}" data-k="state">${['started','completed','approved'].map(x=>`<option ${d.state===x?'selected':''}>${x}</option>`).join('')}</select>
        <button class="btn icon gh" data-a="dep-del" data-i="${i}" aria-label="Remove">${ic('x')}</button></div>`).join('')}
      ${others.length?`<button class="btn sm mt" data-a="dep-add">${ic('plus')}Prerequisite</button>`:''}
      <div class="fgrid mt">
        <div class="fld full"><label>Approval</label><select data-st="approvalRoleId"><option value="">None</option>${S.active('roles').map(r=>`<option value="${r.id}" ${s.approvalRoleId===r.id?'selected':''}>${esc(r.name)}</option>`).join('')}</select></div>
        <div class="fld"><label>Wait (days)</label><input data-st="waitDays" value="${esc(val(s,'waitDays','waitSetting')||'')}" inputmode="numeric" ${S.get('settings',s.waitSetting)?'disabled':''}></div>
        <div class="fld"><label>From setting</label><select data-st="waitSetting"><option value="">Fixed</option>${S.active('settings').filter(x=>x.unit==='days').map(x=>`<option value="${x.id}" ${s.waitSetting===x.id?'selected':''}>${esc(x.name)}</option>`).join('')}</select></div>
        <div class="fld"><label>Check every (hours)</label><input data-st="repeatHours" value="${esc(val(s,'repeatHours','repeatSetting')||'')}" inputmode="numeric" ${S.get('settings',s.repeatSetting)?'disabled':''}></div>
        <div class="fld"><label>From setting</label><select data-st="repeatSetting"><option value="">Fixed</option>${S.active('settings').filter(x=>x.unit==='hours').map(x=>`<option value="${x.id}" ${s.repeatSetting===x.id?'selected':''}>${esc(x.name)}</option>`).join('')}</select></div>
      </div>
      <div class="row"><button class="btn sm" data-a="stage-add" data-after="${s.id}">${ic('plus')}Stage after</button><span class="sp"></span><button class="btn sm dngo" data-a="stage-del">${ic('trash')}Delete stage</button></div></div>`;
  }

  function trySide(m,T){
    if(!T.cohort)T.cohort=Object.assign({offered:'',selected:'',boarded:'',received:''},m.cohort||{});
    const rows=m.stages.map((s,i)=>{const [l,t]=stageState(m,s,T);const x=T.st[s.id]||{};const b=[];
      if(l==='Ready')b.push(`<button class="btn sm p" data-a="try-do" data-s="${s.id}" data-v="start">Start</button>`);
      if(l==='Waiting')b.push(`<button class="btn sm" data-a="try-do" data-s="${s.id}" data-v="wait">Mark ${val(s,'waitDays','waitSetting')} d elapsed</button>`);
      if(x.started&&!x.completed&&s.repeatHours)b.push(`<button class="btn sm" data-a="try-do" data-s="${s.id}" data-v="check">Check ${(x.checks||0)+1}</button>`);
      if(l==='In progress')b.push(`<button class="btn sm p" data-a="try-do" data-s="${s.id}" data-v="complete">Complete</button>`);
      if(l==='Awaiting approval')b.push(`<button class="btn sm p" data-a="try-do" data-s="${s.id}" data-v="approve">Approve · ${esc(REG.labelById('roles',s.approvalRoleId))}</button>`);
      const blocked=l==='Locked'?s.deps.filter(d=>{const y=T.st[d.stage]||{};return !(d.state==='started'?y.started:d.state==='completed'?y.completed:y.approved);}).map(d=>(m.stages.find(z=>z.id===d.stage)||{}).label+' '+d.state).join(', '):'';
      return `<tr><td class="muted small">${i+1}</td><td class="trymain"><div class="tryname" title="${esc(s.label)}">${esc(s.label)}</div>${blocked?`<div class="small muted">Needs ${esc(blocked)}</div>`:''}${x.checks?`<div class="small muted">${x.checks} checks</div>`:''}
        <div class="trysub">${UI.tag(l,t)}<span class="sp"></span>${b.join('')}</div></td></tr>`;}).join('');
    const c=T.cohort;
    return `<div class="hd"><h3>Try run</h3><span class="sp"></span><button class="btn sm" data-a="try-reset">${ic('rotate')}Reset</button></div>
      <div class="trycoh">${['offered','selected','boarded','received'].map(k=>`<label>${k[0].toUpperCase()+k.slice(1)}<input class="inl num" data-coh="${k}" value="${esc(c[k])}" placeholder="0" inputmode="numeric"></label>`).join('')}</div>
      <div class="twrap screen trylist"><table><tbody>${rows}</tbody></table></div>`;
  }

  M.mount=function(root,id){
    const m=S.get('masters',id); if(!m)return;
    if(Flow.readOnly()&&!root.querySelector('.trycoh')){Flow.lockPanel(root.querySelector('.insp'));return;}
    const s=m.stages.find(x=>x.id===M.sel[id]);
    root.querySelectorAll('[data-ms]').forEach(i=>i.onchange=()=>{m[i.dataset.ms]=i.value;S.save();App.render();});
    root.querySelectorAll('[data-coh]').forEach(i=>i.oninput=()=>{tr(id).cohort[i.dataset.coh]=i.value;});
    if(!s)return;
    root.querySelectorAll('[data-st]').forEach(i=>i.onchange=()=>{
      const k=i.dataset.st;
      if(k==='sopId'&&i.value==='__new'){S.snap('New SOP');const c=S.add('sops',{dept:m.dept,title:s.label,category:'action',nodes:[{id:'n1',type:'start',label:'Start',next:[{to:'n2'}]},{id:'n2',type:'end',label:'Done',outcome:'done',next:[]}],versions:[]});s.sopId=c.id;S.save();App.render();UI.toast('Created SOP “'+s.label+'”');return;}
      s[k]=(k==='waitDays'||k==='repeatHours')?(i.value===''?'':Number(i.value)):i.value;
      if(k==='waitSetting'){const st=S.get('settings',i.value);if(st)s.waitDays=st.value;}
      if(k==='repeatSetting'){const st=S.get('settings',i.value);if(st)s.repeatHours=st.value;}
      S.save();App.render();});
    root.querySelectorAll('[data-dep]').forEach(i=>i.onchange=()=>{s.deps[+i.dataset.dep][i.dataset.k]=i.value;S.save();App.render();});
  };

  const cur=()=>S.get('masters',App.route.params.id);
  Object.assign(A,{
    'stage-sel'(el){M.sel[App.route.params.id]=el.dataset.id||null;App.render();},
    'stage-add'(el){const m=cur();S.snap('Stage');const id='s'+Date.now().toString(36);const after=el.dataset.after;
      const st={id,label:'New stage',sopId:'',deps:after?[{stage:after,state:'completed'}]:[],approvalRoleId:'',waitDays:'',waitSetting:'',repeatHours:'',repeatSetting:''};
      const ix=after?m.stages.findIndex(x=>x.id===after)+1:m.stages.length;m.stages.splice(ix,0,st);M.sel[m.id]=id;S.save();App.render();},
    'stage-del'(){const m=cur();const id=M.sel[m.id];UI.undoable('Stage deleted',()=>{m.stages=m.stages.filter(x=>x.id!==id);m.stages.forEach(x=>x.deps=x.deps.filter(d=>d.stage!==id));M.sel[m.id]=null;App.render();});},
    'dep-add'(){const m=cur();const s=m.stages.find(x=>x.id===M.sel[m.id]);const o=m.stages.find(x=>x.id!==s.id&&!s.deps.some(d=>d.stage===x.id));if(!o)return;s.deps.push({stage:o.id,state:'completed'});S.save();App.render();},
    'dep-del'(el){const m=cur();const s=m.stages.find(x=>x.id===M.sel[m.id]);s.deps.splice(+el.dataset.i,1);S.save();App.render();},
    'mst-publish'(el){const m=S.get('masters',el.dataset.id);const iss=validate(m);if(iss.length){UI.toast('Fix '+iss.length+' issues before publishing');return;}
      if(!pending(m).changed){UI.toast('No changes to publish');return;}
      const prev=m.versions[m.versions.length-1];S.snap('Publish');m.versions.push({v:(prev?prev.v:0)+1,at:new Date().toISOString(),by:'Ravi Teja',running:0,title:m.title,stages:JSON.parse(JSON.stringify(m.stages)),pins:Flow.pinNow(m.stages)});S.save();App.render();
      UI.toast('Published v'+(prev?prev.v+1:1)+(prev&&prev.running?' · '+prev.running+' running stay on v'+prev.v:''),()=>{S.undo();App.render();});},
    'try-do'(el){const m=cur(),T=tr(m.id),x=T.st[el.dataset.s]||(T.st[el.dataset.s]={});const v=el.dataset.v;
      if(v==='start')x.started=true;if(v==='wait')x.waitDone=true;if(v==='check')x.checks=(x.checks||0)+1;if(v==='complete')x.completed=true;if(v==='approve')x.approved=true;
      App.render();},
    'try-reset'(){M.try[App.route.params.id]=null;App.render();}
  });

  /* ---------- master stages on the shared canvas engine ---------- */
  const STATE_TXT={completed:'After completed',approved:'After approved',started:'Parallel · started'};
  const stg=(m,id)=>m.stages.find(x=>x.id===id);
  const ek=key=>{const i=key.lastIndexOf(':');return [key.slice(0,i),+key.slice(i+1)];};
  const newStage=(m,label,deps)=>({id:'s'+Date.now().toString(36)+Math.random().toString(36).slice(2,5),label:label||'New stage',sopId:'',deps:deps||[],approvalRoleId:'',waitDays:'',waitSetting:'',repeatHours:'',repeatSetting:''});
  function positions(m){
    const auto=layout(m.stages).pos, saved=m.layout||{}, pos={};
    m.stages.forEach(s=>{if(saved[s.id])pos[s.id]={x:saved[s.id].x,y:saved[s.id].y};});
    if(!Object.keys(pos).length){m.stages.forEach(s=>pos[s.id]={x:auto[s.id].x,y:auto[s.id].y});return pos;}
    m.stages.forEach(s=>{if(pos[s.id])return;
      const pre=s.deps.map(d=>pos[d.stage]).filter(Boolean);let x=pre.length?pre[0].x:auto[s.id].x,y=pre.length?Math.max(...pre.map(p=>p.y))+200:Math.max(0,...Object.values(pos).map(p=>p.y))+200;
      let g=0;while(Object.values(pos).some(p=>Math.abs(p.x-x)<280&&Math.abs(p.y-y)<150)&&g++<30)x+=300;pos[s.id]={x,y};});
    return pos;}
  const persistPos=(m,pos)=>{m.layout=m.layout||{};Object.keys(pos).forEach(id=>m.layout[id]={x:pos[id].x,y:pos[id].y});};
  Flow.registerKind('master',{
    get:id=>S.get('masters',id), noun:'stage',
    nodes:m=>m.stages.map(s=>({id:s.id,type:'stage',label:s.label,deps:s.deps})),
    edges:m=>{const out=[];m.stages.forEach(s=>s.deps.forEach((d,j)=>out.push({key:s.id+':'+j,from:d.stage,to:s.id,label:STATE_TXT[d.state]||d.state,cls:'dep-'+d.state})));return out;},
    estH:n=>{const s=n;return 112;},
    positions,
    getSel:m=>M.sel[m.id]||null, setSel:(m,id)=>{M.sel[m.id]=id||null;},
    snapshot:m=>JSON.stringify({stages:m.stages,layout:m.layout||null,title:m.title}),
    restore(m,x){const o=JSON.parse(x);m.stages=o.stages;if(o.layout)m.layout=o.layout;else delete m.layout;m.title=o.title;},
    palette:['stage'], typeLabel:()=>'Stage', typeIcon:()=>'layers',
    validate:m=>validate(m).map(msg=>({msg})),
    starts:m=>m.stages.filter(s=>!s.deps.length).map(s=>s.id),
    nodeClass:(n,m)=>{const s=stg(m,n.id);return 'k-stage'+(s&&s.deps.some(d=>d.state==='started')?' par':'');},
    nodeInner(m,n,i,opts){const s=stg(m,n.id);const c=S.get('sops',s.sopId);const v=m.versions[m.versions.length-1];
      const par=s.deps.some(d=>d.state==='started');let run='';
      if(opts&&opts.view==='try'){const [l,t]=stageState(m,s,tr(m.id));run=UI.tag(l,t);}
      const tags=[c&&!(c.versions||[]).length?UI.tag('Draft','warn'):'',s.approvalRoleId?UI.tag('Approval · '+REG.labelById('roles',s.approvalRoleId),'pur'):'',
        s.waitDays||s.waitSetting?UI.tag('Wait '+val(s,'waitDays','waitSetting')+' d','warn'):'',
        s.repeatHours||s.repeatSetting?UI.tag('Every '+val(s,'repeatHours','repeatSetting')+' h','warn'):''].join('');
      return `<div class="fx-eyebrow">${ic(par?'split':'layers','',13)}<span>Stage ${i+1}</span>${run}</div>
        <div class="fx-title">${esc(s.label||'Untitled')}</div>
        <div class="fx-sum">${c?esc(c.title)+' · '+Flow.stepCount(c.nodes)+' steps':'No child SOP'}</div>
        ${tags?`<div class="fx-tags">${tags}</div>`:''}
        ${Flow.settingChips?`<div class="mst-sets">${Flow.settingChips({waitSetting:s.waitSetting,repeatSetting:s.repeatSetting},v)}</div>`:''}`;},
    defaultEdge:'completed',
    askEdge(m,from,to,cx,cy,done){setTimeout(()=>{const a=document.createElement('div');a.style.cssText=`position:fixed;left:${cx}px;top:${cy}px;width:0;height:0`;document.body.appendChild(a);
      UI.menu(a,['completed','approved','started'].map(st=>({label:STATE_TXT[st],icon:st==='started'?'split':st==='approved'?'check':'flag',run:()=>done(st)})));a.remove();
      const mm=document.getElementById('menu');if(mm){mm.style.left=Math.max(8,Math.min(cx,innerWidth-mm.offsetWidth-8))+'px';mm.style.top=Math.min(cy+4,innerHeight-mm.offsetHeight-8)+'px';}},0);},
    connect(m,from,to,state){const a=stg(m,from),b=stg(m,to);if(!a||!b)return 'Cannot connect';if(from===to)return 'A stage cannot wait on itself';
      if(b.deps.some(d=>d.stage===from))return 'Already connected';
      if(state==='approved'&&!a.approvalRoleId)UI.toast(a.label+' has no approval yet · set one in the inspector');
      b.deps.push({stage:from,state:state||'completed'});return true;},
    reconnect(m,key,end,id){const [sid,j]=ek(key);const s=stg(m,sid);const d=s&&s.deps[j];if(!d)return 'Connection not found';
      if(end==='t'){const x=stg(m,id);if(!x)return 'Cannot connect';if(id===d.stage)return 'A stage cannot wait on itself';if(x.deps.some(y=>y.stage===d.stage))return 'Already connected';
        s.deps.splice(j,1);x.deps.push(d);return true;}
      if(id===sid)return 'A stage cannot wait on itself';if(s.deps.some((y,k)=>k!==j&&y.stage===id))return 'Already connected';d.stage=id;return true;},
    deleteEdge(m,key){const [sid,j]=ek(key);const s=stg(m,sid);if(s)s.deps.splice(j,1);},
    edgeEditLabel:'Change dependency',
    editEdgeOnClick:true,
    editEdge(m,key,cx,cy,done){const [sid,j]=ek(key);const s=stg(m,sid);const d=s&&s.deps[j];if(!d)return;
      setTimeout(()=>{const a=document.createElement('div');a.style.cssText=`position:fixed;left:${cx}px;top:${cy}px;width:0;height:0`;document.body.appendChild(a);
        UI.menu(a,['completed','approved','started'].map(st=>({label:(d.state===st?'✓ ':'')+STATE_TXT[st],icon:st==='started'?'split':st==='approved'?'check':'flag',run:()=>{if(d.state!==st){S.snap('Dependency');d.state=st;done();}}})).concat(['-',{label:'Delete connection',icon:'trash',danger:true,run:()=>{S.snap('Delete connection');s.deps.splice(j,1);Flow._edgeSel=null;done();}}]));a.remove();
        const mm=document.getElementById('menu');if(mm){mm.style.left=Math.max(8,Math.min(cx,innerWidth-mm.offsetWidth-8))+'px';mm.style.top=Math.min(cy+4,innerHeight-mm.offsetHeight-8)+'px';}},0);},
    removeNodes(m,ids){const set=new Set(ids);const n=m.stages.filter(x=>set.has(x.id)).length;m.stages=m.stages.filter(x=>!set.has(x.id));m.stages.forEach(x=>x.deps=x.deps.filter(d=>!set.has(d.stage)));if(m.layout)ids.forEach(id=>delete m.layout[id]);return n;},
    disconnect(m,ids){const set=new Set(ids);m.stages.forEach(x=>x.deps=set.has(x.id)?[]:x.deps.filter(d=>!set.has(d.stage)));},
    duplicate(m,ids){const pos=positions(m);const out=[];ids.forEach(id=>{const s=stg(m,id);if(!s)return;const c=Object.assign(JSON.parse(JSON.stringify(s)),{id:newStage(m).id,label:s.label+' copy'});
      m.stages.push(c);pos[c.id]={x:pos[id].x+40,y:pos[id].y+40};out.push(c.id);});persistPos(m,pos);return out;},
    addAt(m,type,x,y){const pos=positions(m);const s=newStage(m);m.stages.push(s);pos[s.id]={x:Math.round((x-132)/20)*20,y:Math.round((y-40)/20)*20};persistPos(m,pos);return s.id;},
    addAfter(m,type,after){const pos=positions(m);const a=stg(m,after);const s=newStage(m,'',[{stage:after,state:'completed'}]);
      m.stages.splice(m.stages.indexOf(a)+1,0,s);const p=pos[after]||{x:0,y:0};let x=p.x,y=p.y+200;let g=0;while(Object.values(pos).some(q=>Math.abs(q.x-x)<280&&Math.abs(q.y-y)<150)&&g++<30)x+=300;pos[s.id]={x,y};persistPos(m,pos);return s.id;},
    paletteClick(m,type,center){const sel=M.sel[m.id];return sel&&stg(m,sel)?this.addAfter(m,type,sel):this.addAt(m,type,center.x,center.y);},
    paletteDrop(m,type,w,over){return over?this.addAfter(m,type,over):this.addAt(m,type,w.x,w.y);}
  });
  window.Master=M;
})();
