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
    const hOf=s=>84+26*(s.deps.length+(s.approvalRoleId?1:0)+(s.waitDays?1:0)+(s.repeatHours?1:0));
    const maxR=Math.max(0,...Object.values(rank)); const rowY=[]; let y=34;
    for(let r=0;r<=maxR;r++){rowY[r]=y;y+=Math.max(112,...stages.filter(s=>rank[s.id]===r).map(hOf))+56;}
    const pos={};let maxc=0;stages.forEach(s=>{pos[s.id]={x:34+col[s.id]*300,y:rowY[rank[s.id]],w:260,h:Math.max(112,hOf(s))};maxc=Math.max(maxc,col[s.id]);});
    return {pos,W:34+(maxc+1)*300,H:y-22};
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

  M.page=function(id,view){
    const m=S.get('masters',id); if(!m)return '<div class="empty">Not found</div>';
    const iss=validate(m); const v=m.versions[m.versions.length-1]; const T=tr(id);
    const L=layout(m.stages); const by={};m.stages.forEach(s=>by[s.id]=s);
    let edges='';
    m.stages.forEach(s=>s.deps.forEach(d=>{const a=L.pos[d.stage],b=L.pos[s.id];if(!a||!b)return;
      let path;
      if(d.state==='started'&&a.y===b.y)path=`M${a.x+a.w},${a.y+56} L${b.x-4},${b.y+56}`;
      else{const sy=a.y+a.h+2;path=`M${a.x+a.w/2},${sy} C${a.x+a.w/2},${sy+26} ${b.x+b.w/2},${b.y-26} ${b.x+b.w/2},${b.y-3}`;}
      edges+=`<path class="dep-${d.state}" d="${path}" marker-end="url(#mah)"/>`;}));
    const nodes=m.stages.map((s,i)=>{const p=L.pos[s.id];const c=S.get('sops',s.sopId);const [stl,stt]=stageState(m,s,T);
      const par=s.deps.some(d=>d.state==='started');
      return `<div class="node k-stage ${M.sel[id]===s.id?'sel':''}" style="left:${p.x}px;top:${p.y}px;min-height:${p.h}px;${par?'--k:var(--info)':''}" data-a="stage-sel" data-id="${s.id}">
        <div class="nt">${ic(par?'split':'layers')}Stage ${i+1}${view==='try'?`<span class="sp"></span>${UI.tag(stl,stt)}`:''}</div>
        <div class="nl">${esc(s.label)}</div>
        <div class="small muted">${c?esc(c.title)+' · '+Flow.stepCount(c.nodes)+' steps':'No child SOP'}</div>
        <div class="nb">${s.deps.map(d=>UI.tag('After '+((by[d.stage]||{}).label||'?')+' '+STATE_L[d.state],d.state==='started'?'info':d.state==='approved'?'pur':'mut')).join('')}
          ${s.approvalRoleId?UI.tag('Approval · '+REG.labelById('roles',s.approvalRoleId),'pur'):''}
          ${s.waitDays?UI.tag('Wait '+val(s,'waitDays','waitSetting')+' d','warn'):''}${s.repeatHours?UI.tag('Every '+val(s,'repeatHours','repeatSetting')+' h','warn'):''}</div></div>`;}).join('');
    const canvas=`<div class="canvas" style="height:auto;min-height:${Math.min(L.H+20,900)}px"><div class="board" style="width:${L.W}px;height:${L.H}px;position:relative">
      <svg class="edges" width="${L.W}" height="${L.H}"><defs><marker id="mah" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" style="fill:var(--muted);stroke:none"/></marker></defs>${edges}</svg>${nodes}</div></div>`;
    const head=`<div class="phead"><div><div class="crumb">Configuration / <a href="#/configuration/work-instructions">Work instructions</a> / <b>${esc(m.dept)}</b></div><h1>${esc(m.title)}</h1>
      <div class="vstrip" style="margin-top:6px">${v?UI.tag('v'+v.v+' published','ok'):''}${v&&v.running?UI.tag(v.running+' running','info'):''}${UI.tag(m.stages.length+' stages','mut')}${iss.length?UI.tag(iss.length+' issues','dng'):''}</div></div><div class="sp"></div>
      <div class="seg"><a class="${view!=='try'?'on':''}" href="${base}${id}">${ic('layers')}Stages</a><a class="${view==='try'?'on':''}" href="${base}${id}/try">${ic('play')}Try</a></div>
      <button class="btn p" data-a="mst-publish" data-id="${id}" ${iss.length?'disabled':''}>${ic('check')}Publish v${v?v.v+1:1}</button></div>`;
    const side=view==='try'?trySide(m,T):inspector(m,iss);
    return head+`<div class="editor"><section class="card"><div class="canvasbar"><div class="legend"><span><i></i>Completed</span><span><i class="a"></i>Approved</span><span><i class="s"></i>Started (parallel)</span></div><span class="sp"></span>
      ${view!=='try'?`<button class="btn sm" data-a="stage-add">${ic('plus')}Stage</button>`:''}</div>${canvas}</section><section class="card insp">${side}</section></div>`;
  };

  function inspector(m,iss){
    const s=m.stages.find(x=>x.id===M.sel[m.id]);
    if(!s)return `<div class="hd"><h3>Master SOP</h3></div><div class="bd">
      <div class="fld"><label>Title</label><input data-ms="title" value="${esc(m.title)}"></div>
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
    if(!T.cohort)T.cohort=Object.assign({},m.cohort||{offered:100,selected:70,boarded:0,received:0});
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
      <div class="trycoh">${['offered','selected','boarded','received'].map(k=>`<label>${k[0].toUpperCase()+k.slice(1)}<input class="inl num" data-coh="${k}" value="${esc(c[k])}" inputmode="numeric"></label>`).join('')}</div>
      <div class="twrap screen trylist"><table><tbody>${rows}</tbody></table></div>`;
  }

  M.mount=function(root,id){
    const m=S.get('masters',id); if(!m)return;
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
      const prev=m.versions[m.versions.length-1];S.snap('Publish');m.versions.push({v:(prev?prev.v:0)+1,at:new Date().toISOString(),by:'Ravi Teja',running:0,stages:JSON.parse(JSON.stringify(m.stages))});S.save();App.render();
      UI.toast('Published v'+(prev?prev.v+1:1)+(prev&&prev.running?' · '+prev.running+' running stay on v'+prev.v:''),()=>{S.undo();App.render();});},
    'try-do'(el){const m=cur(),T=tr(m.id),x=T.st[el.dataset.s]||(T.st[el.dataset.s]={});const v=el.dataset.v;
      if(v==='start')x.started=true;if(v==='wait')x.waitDone=true;if(v==='check')x.checks=(x.checks||0)+1;if(v==='complete')x.completed=true;if(v==='approve')x.approved=true;
      App.render();},
    'try-reset'(){M.try[App.route.params.id]=null;App.render();}
  });
  window.Master=M;
})();
