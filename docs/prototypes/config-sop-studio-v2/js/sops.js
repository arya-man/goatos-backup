/* Configuration / Work instructions: SOP list, flowchart editor, inspector, versions */
(function(){
  const W={sel:{},q:'',dept:''};
  const DICON={Procurement:'truck',Weighing:'scale',Feed:'wheat',Health:'stethoscope',Counts:'bar-chart-3','Preventive Care':'heart-pulse',Sales:'banknote',Milk:'edit-3',People:'user'};
  const base='#/configuration/work-instructions';
  W.base=base;

  function sopCard(s){
    const v=Flow.latest(s), ch=Flow.changed(s), steps=Flow.stepCount(s.nodes);
    const n=t=>s.nodes.filter(x=>x.type===t).length;
    return `<a class="card sopcard" href="${base}/${s.id}"><h3>${esc(s.title)}</h3>
      <div class="nums">${[[steps,'step'],[Flow.pages(s.nodes).length,'page'],[n('approval'),'approval'],[n('evidence'),'proof']].map(([c,w])=>`<span><b>${c}</b>${w}${c===1?'':'s'}</span>`).join('')}</div>
      <div class="meta">${v?UI.tag('v'+v.v+' published','ok'):UI.tag('Draft','warn')}${ch&&v?UI.tag('Draft changes','warn'):''}${v&&v.running?UI.tag(v.running+' running','info'):''}</div></a>`;
  }
  function masterCard(mst){
    const v=mst.versions[mst.versions.length-1]; const par=mst.stages.filter(s=>s.deps.some(d=>d.state==='started')).length;
    return `<a class="card sopcard" href="${base}/master/${mst.id}"><div class="eyebrow">${ic('layers','',13)} Master SOP · ${esc(mst.dept)}</div><h3>${esc(mst.title)}</h3>
      <div class="nums">${[[mst.stages.length,'stage'],[mst.stages.filter(s=>s.approvalRoleId).length,'approval']].map(([c,w])=>`<span><b>${c}</b>${w}${c===1?'':'s'}</span>`).join('')}<span><b>${par}</b>parallel</span></div>
      <div class="meta">${v?UI.tag('v'+v.v+' published','ok'):''}${v&&v.running?UI.tag(v.running+' running','info'):''}</div></a>`;
  }

  W.listPage=function(presetDept,crumb){
    const sops=S.all('sops').filter(s=>s.status!=='archived');
    const dept=presetDept||W.dept; const q=REG.norm(W.q);
    const depts=[...new Set(sops.map(s=>s.dept))].sort((a,b)=>DEPTS.indexOf(a)-DEPTS.indexOf(b));
    const shown=sops.filter(s=>(!dept||s.dept===dept)&&(!q||REG.norm(s.title+' '+s.dept).includes(q)));
    const masters=S.all('masters').filter(m=>m.status!=='archived'&&(!dept||m.dept===dept)&&(!q||REG.norm(m.title).includes(q)));
    const running=sops.reduce((a,s)=>a+((Flow.latest(s)||{}).running||0),0);
    const k=(l,v,sub)=>`<div class="kpi stat"><div class="lab">${l}</div><div class="val">${v}</div><div class="dl">${sub}</div></div>`;
    return `<div class="phead"><div><div class="crumb">${crumb||'Configuration'} / <b>${presetDept?esc(presetDept)+' SOP':'Work instructions'}</b></div><h1>${presetDept?esc(presetDept)+' SOP':'Work instructions'}</h1></div><div class="sp"></div>
      <button class="btn out" data-a="sop-new" data-dept="${esc(presetDept||'')}">${ic('plus')}New SOP</button></div>
      <div class="lbar card" style="margin-bottom:6px"><label class="search">${ic('search')}<input data-sopq value="${esc(W.q)}" placeholder="Search SOPs"></label>
        ${presetDept?'':`<div class="chips"><button class="chip ${dept?'':'on'}" data-a="sop-dept" data-v="">All</button>${depts.map(d=>`<button class="chip ${dept===d?'on':''}" data-a="sop-dept" data-v="${esc(d)}">${esc(d)} <span class="muted">${sops.filter(s=>s.dept===d).length}</span></button>`).join('')}</div>`}</div>
      ${masters.length?`<div class="dhead">${ic('layers')}<h2>Master SOPs</h2><span class="cnt">${masters.length}</span></div><div class="sopgrid">${masters.map(masterCard).join('')}</div>`:''}
      ${depts.filter(d=>shown.some(s=>s.dept===d)).map(d=>`<div class="dhead">${ic(DICON[d]||'workflow')}<h2>${esc(d)}</h2><span class="cnt">${shown.filter(s=>s.dept===d).length}</span></div>
        <div class="sopgrid">${shown.filter(s=>s.dept===d).map(sopCard).join('')}</div>`).join('')||(presetDept?'':'<div class="empty">No matches</div>')}`;
  };
  document.addEventListener('input',e=>{if(e.target.matches&&e.target.matches('[data-sopq]')){W.q=e.target.value;const p=e.target.selectionStart;App.render();const n=document.querySelector('[data-sopq]');if(n){n.focus();n.setSelectionRange(p,p);}}});

  Object.assign(A,{
    'sop-dept'(el){W.dept=el.dataset.v;App.render();},
    'sop-new'(el){
      const f={title:'',dept:el.dataset.dept||'',from:''};
      UI.drawer({title:'New SOP',body:`<div class="fld"><label>Title *</label><input data-ns="title" placeholder="Hoof trimming"></div>
        <div class="fld"><label>Department *</label><input data-ns="dept" value="${esc(f.dept)}" placeholder="Search or create" autocomplete="off"></div>
        <div class="fld"><label>Start from</label><select data-ns="from"><option value="">Blank</option>${S.active('sops').map(s=>`<option value="${s.id}">${esc(s.dept)} · ${esc(s.title)}</option>`).join('')}</select></div>`,
        foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-nssave>Create</button>`,
        mount(dr){
          dr.querySelectorAll('[data-ns]').forEach(i=>{i.oninput=i.onchange=()=>{f[i.dataset.ns]=i.value;if(i.dataset.ns==='dept')pop(i);};if(i.dataset.ns==='dept'){i.onfocus=()=>pop(i);i.onkeydown=e=>UI.popKey(e);i.onblur=()=>setTimeout(UI.closePop,150);}});
          function pop(i){const ds=[...new Set(DEPTS.concat(S.all('sops').map(s=>s.dept)))];UI.pop(i,{options:()=>ds.map(d=>({id:d,label:d})),onPick(){f.dept=i.value;},onCreate:t=>({id:t,label:t})});}
          dr.querySelector('[data-nssave]').onclick=()=>{
            if(!f.title.trim()||!f.dept.trim()){UI.toast('Title and department are required');return;}
            S.snap('New SOP');
            const src=S.get('sops',f.from);
            const nodes=src?JSON.parse(JSON.stringify(src.nodes)):[{id:'n1',type:'start',label:'Start',next:[{to:'n2'}]},{id:'n2',type:'end',label:'Done',outcome:'done',next:[]}];
            const s=S.add('sops',{dept:f.dept.trim(),title:f.title.trim(),category:src?src.category:'action',nodes,versions:[]});
            S.save(); UI.closeDrawer(); location.hash=base+'/'+s.id;
          };
        }});
    }
  });

  /* ---------- editor ---------- */
  function vtags(s){const v=Flow.latest(s),ch=Flow.changed(s);return (v?UI.tag('v'+v.v+' published','ok'):UI.tag('Never published','warn'))+(v&&ch?UI.tag('Draft changes','warn'):'')+(v&&v.running?UI.tag(v.running+' running on v'+v.v,'info'):'');}
  W.editorPage=function(id,view){
    const s=S.get('sops',id); if(!s)return '<div class="empty">SOP not found</div>';
    const iss=Flow.validate(s); const v=Flow.latest(s); const ch=Flow.changed(s);
    const head=`<div class="phead"><div><div class="crumb">Configuration / <a href="${base}">Work instructions</a> / <b>${esc(s.dept)}</b></div><h1>${esc(s.title)}</h1>
      <div class="vstrip" style="margin-top:6px">${vtags(s)}${UI.tag(Flow.stepCount(s.nodes)+' steps','mut')}${iss.length?UI.tag(iss.length+' issues','dng'):''}</div></div><div class="sp"></div>
      <div class="seg"><a class="${!view?'on':''}" href="${base}/${s.id}">${ic('workflow')}Flowchart</a><a class="${view==='list'?'on':''}" href="${base}/${s.id}/list">${ic('list-checks')}List</a><a class="${view==='operator'?'on':''}" href="${base}/${s.id}/operator">${ic('smartphone')}Operator view</a></div>
      <button class="btn" data-a="sop-versions" data-id="${s.id}">${ic('history')}Versions</button>
      <button class="btn p" data-a="sop-publish" data-id="${s.id}" ${ch?'':'disabled'}>${ic('check')}Publish v${v?v.v+1:1}${iss.length?` <span class="tag t-dng">${iss.length}</span>`:''}</button></div>`;
    if(view==='operator')return head+'<div data-meshaop></div>';
    if(view==='list')return head+`<div class="editor"><section class="card">${listView(s,W.sel[s.id])}</section><section class="card insp">${inspector(s,W.sel[s.id],iss)}</section></div>`;
    const sel=W.sel[s.id]; const run=window.MeshaOperator&&MeshaOperator.runMap?MeshaOperator.runMap(s):null;
    return head+`<div class="editor"><section class="card"><div class="canvasbar">
        <span class="sp"></span>
        <div class="legend">${['question','evidence','decision','approval','wait','repeat','child','parallel'].map(t=>`<span class="k-${t}" style="color:var(--k)">${ic(Flow.TYPES[t].icon,'',13)} ${Flow.TYPES[t].label}</span>`).join('')}</div>
      </div><div class="canvas" data-canvas>${Flow.canvasHtml(s,sel,run)}</div></section>
      <section class="card insp">${inspector(s,sel,iss)}</section></div>`;
  };

  function listView(s,sel){
    const L=Flow.layout(s.nodes); const m=Flow.byId(s.nodes);
    const order=[...s.nodes].sort((a,b)=>((L.rank[a.id]||0)-(L.rank[b.id]||0))||(L.order.indexOf(a.id)-L.order.indexOf(b.id)));
    let lastGroup=null,i=0;
    const nextTxt=n=>(n.next||[]).map(e=>(e.when?e.when+' → ':'→ ')+((m[e.to]||{}).label||'—')).join(' · ');
    const rows=order.map(n=>{
      const g=n.page||n.lane||''; let head='';
      if(g!==lastGroup){head=g?`<div class="lgroup eyebrow">${esc(g)}</div>`:'';lastGroup=g;}
      const T=Flow.TYPES[n.type]||Flow.TYPES.action; const step=['start','end','join','parallel'].includes(n.type)?'':++i;
      const sub=[n.type==='question'?(Flow.ANSWERS[n.answer]||'')+(n.unit?' · '+n.unit:''):'',n.required?'Required':'',n.reject?'Reject rule':'',n.onlyIf?'Conditional':'',
        n.type==='approval'?REG.labelById('roles',n.roleId):'',n.type==='evidence'?(n.media||[]).join('/')+' '+(n.min||0)+'–'+(n.max||1):'',
        n.type==='child'?((S.get('sops',n.sopId)||{}).title||'Not set'):''].filter(Boolean).join(' · ');
      return head+`<div class="lstep k-${n.type} ${sel===n.id?'sel':''}" data-a="node-sel" data-id="${n.id}">
        <span class="lnum">${step}</span><div class="lbody"><div class="row nw"><span class="ltype">${ic(T.icon,'',13)}${T.label}</span><b class="lttl">${esc(n.type==='child'&&S.get('sops',n.sopId)?S.get('sops',n.sopId).title:n.label||T.label)}</b></div>
        ${sub?`<div class="small muted">${esc(sub)}</div>`:''}${(n.next||[]).length?`<div class="small muted">${esc(nextTxt(n))}</div>`:''}</div>
        ${n.type!=='end'?`<button class="btn sm" data-a="list-ins" data-id="${n.id}">${ic('plus')}Insert</button>`:''}</div>`;
    }).join('');
    return `<div class="hd"><h3>Steps <span class="cnt">${Flow.stepCount(s.nodes)}</span></h3></div><div class="lsteps">${rows}</div>`;
  }
  function roleOpts(val){return S.active('roles').map(r=>`<option value="${r.id}" ${val===r.id?'selected':''}>${esc(r.name)}</option>`).join('');}
  function settingOpts(val,units){return `<option value="">Fixed value</option>`+S.active('settings').filter(x=>!units||units.includes(x.unit)).map(x=>`<option value="${x.id}" ${val===x.id?'selected':''}>${esc(x.dept)} · ${esc(x.name)} (${esc(x.value)} ${esc(x.unit)})</option>`).join('');}
  function nodeOpts(s,val,filter){return s.nodes.filter(filter||(()=>true)).map(n=>`<option value="${n.id}" ${val===n.id?'selected':''}>${esc((Flow.TYPES[n.type]||{}).label+' · '+(n.label||''))}</option>`).join('');}
  const fld=(label,inner,full)=>`<div class="fld ${full?'full':''}"><label>${label}</label>${inner}</div>`;

  function inspector(s,selId,iss){
    const n=s.nodes.find(x=>x.id===selId);
    if(!n){
      return `<div class="hd"><h3>SOP</h3><span class="sp"></span><button class="btn sm" data-a="sop-more" data-id="${s.id}">${ic('more')}</button></div><div class="bd">
        ${fld('Title',`<input data-sp="title" value="${esc(s.title)}">`)}
        ${fld('Department',`<input data-sp="dept" value="${esc(s.dept)}" autocomplete="off">`)}
        ${fld('Category',`<select data-sp="category">${['commodity','problem','event','action','equipment'].map(c=>`<option ${s.category===c?'selected':''}>${c}</option>`).join('')}</select>`)}
        <div class="eyebrow mt">Issues</div>
        ${iss.length?`<div class="stack" style="gap:6px;margin-top:6px">${iss.map(i=>`<button class="btn sm dngo" style="justify-content:flex-start;white-space:normal;text-align:left" data-a="node-sel" data-id="${i.id||''}">${ic('reject')}${esc(i.msg)}</button>`).join('')}</div>`:`<div class="mt">${UI.tag('Ready to publish','ok')}</div>`}
      </div>`;
    }
    const T=Flow.TYPES[n.type]; let f='';
    const lab=fld('Label',`<input data-np="label" value="${esc(n.label)}">`,1);
    const pageF=fld('Page',`<input data-np="page" value="${esc(n.page||'')}" list="pages-${s.id}" placeholder="Group on one screen"><datalist id="pages-${s.id}">${Flow.pages(s.nodes).map(p=>`<option value="${esc(p)}">`).join('')}</datalist>`);
    const next=(n.type!=='end'&&n.type!=='decision'&&n.type!=='parallel')?fld('Next step',`<select data-nnext="0"><option value="">—</option>${nodeOpts(s,(n.next[0]||{}).to,x=>x.id!==n.id&&x.type!=='start')}</select>`):'';
    if(n.type==='question'){
      f=lab+`<div class="fgrid">${fld('Answer',`<select data-np="answer">${Object.entries(Flow.ANSWERS).map(([k,v])=>`<option value="${k}" ${n.answer===k?'selected':''}>${v}</option>`).join('')}</select>`)}
        ${fld('Required',`<select data-np="required" data-bool><option value="1" ${n.required?'selected':''}>Yes</option><option value="" ${n.required?'':'selected'}>No</option></select>`)}
        ${n.answer==='number'?fld('Unit',`<input data-np="unit" value="${esc(n.unit||'')}">`)+fld('Min – max',`<div class="row nw"><input class="inl" style="width:50%" data-np="min" value="${esc(n.min==null?'':n.min)}"><input class="inl" style="width:50%" data-np="max" value="${esc(n.max==null?'':n.max)}"></div>`):''}
        ${n.answer==='ref'?fld('List',`<select data-np="refColl">${Object.entries(Flow.REFS).map(([k,v])=>`<option value="${k}" ${n.refColl===k?'selected':''}>${v}</option>`).join('')}</select>`):''}
        ${pageF}</div>
        ${(n.answer==='choice'||n.answer==='multi')?`<div class="eyebrow">Options</div><table class="mini opts"><tbody>${(n.options||[]).map((o,i)=>`<tr><td><input data-opt="${i}" data-k="l" value="${esc(o.l)}" aria-label="Option label"></td><td class="x"><button class="btn icon gh" data-a="opt-del" data-i="${i}" aria-label="Remove option">${ic('x')}</button></td></tr>`).join('')}</tbody></table>
          <div class="row mt"><button class="btn sm" data-a="opt-add">${ic('plus')}Option</button><label class="row small"><input type="checkbox" data-np="allowOther" data-check ${n.allowOther?'checked':''}>Other with detail</label></div>`:''}
        <div class="eyebrow mt">Show only if</div><div class="fgrid">
          ${fld('Question',`<select data-oi="q"><option value="">Always</option>${nodeOpts(s,n.onlyIf&&n.onlyIf.q,x=>x.type==='question'&&x.id!==n.id&&(x.options||[]).length)}</select>`)}
          ${n.onlyIf&&n.onlyIf.q?fld('Equals',`<select data-oi="v">${((s.nodes.find(x=>x.id===n.onlyIf.q)||{}).options||[]).map(o=>`<option value="${esc(o.v)}" ${n.onlyIf.v===o.v?'selected':''}>${esc(o.l)}</option>`).join('')}</select>`):''}</div>
        <div class="eyebrow mt">Reject rule</div><div class="fgrid">
          ${fld('When',`<select data-rj="op"><option value="">No rule</option><option value="=" ${n.reject&&n.reject.op==='='?'selected':''}>Answer is</option><option value="<" ${n.reject&&n.reject.op==='<'?'selected':''}>Less than</option><option value=">" ${n.reject&&n.reject.op==='>'?'selected':''}>More than</option><option value="cannot" ${n.reject&&n.reject.op==='cannot'?'selected':''}>Can't be answered</option></select>`)}
          ${n.reject&&n.reject.op&&n.reject.op!=='cannot'?fld('Value',(n.options||[]).length&&n.reject.op==='='?`<select data-rj="value">${n.options.map(o=>`<option value="${esc(o.v)}" ${n.reject.value===o.v?'selected':''}>${esc(o.l)}</option>`).join('')}</select>`:`<input data-rj="value" value="${esc(n.reject.value)}">`):''}
          ${n.reject&&n.reject.op?fld('Reason',`<input data-rj="reason" value="${esc(n.reject.reason||'')}">`,1):''}</div>`+next;
    }
    if(n.type==='evidence'){
      f=lab+`<div class="fgrid">${fld('Accepts',`<div class="chkrow">${['photo','video'].map(m=>`<label><input type="checkbox" data-media="${m}" ${(n.media||[]).includes(m)?'checked':''}>${m[0].toUpperCase()+m.slice(1)}</label>`).join('')}</div>`,1)}
        ${fld('Min files',`<input data-np="min" data-num value="${esc(n.min||0)}" inputmode="numeric">`)}${fld('Max files',`<input data-np="max" data-num value="${esc(n.max||1)}" inputmode="numeric">`)}${pageF}</div>`+next;
    }
    if(n.type==='decision'){
      const q=s.nodes.find(x=>x.id===n.q);
      f=lab+`<div class="fgrid">${fld('Question',`<select data-np="q"><option value="">—</option>${nodeOpts(s,n.q,x=>x.type==='question')}</select>`)}
        ${fld('Check',`<select data-np="op">${[['=','Equals'],['≠','Not equal'],['<','Less than'],['>','More than'],['duplicate','Already scanned']].map(([k,l])=>`<option value="${k}" ${n.op===k?'selected':''}>${l}</option>`).join('')}</select>`)}
        ${n.op!=='duplicate'?fld('Value',q&&(q.options||[]).length?`<select data-np="value">${q.options.map(o=>`<option value="${esc(o.v)}" ${n.value===o.v?'selected':''}>${esc(o.l)}</option>`).join('')}</select>`:`<select data-np="value"><option value="${esc(n.value)}">${esc(String(n.value).startsWith('@')?'Answer of '+((s.nodes.find(x=>x.id===n.value.slice(1))||{}).label||''):n.value)}</option>${s.nodes.filter(x=>x.type==='question'&&x.answer==='number'&&x.id!==n.q).map(x=>`<option value="@${x.id}">Answer of ${esc(x.label)}</option>`).join('')}</select>`,1):''}
        ${(n.next||[]).map((e,i)=>fld(esc(e.when||'Branch '+(i+1))+' goes to',`<select data-nnext="${i}">${nodeOpts(s,e.to,x=>x.id!==n.id&&x.type!=='start')}</select>`)).join('')}</div>`;
    }
    if(n.type==='approval'){
      f=lab+fld('Approver role',`<select data-np="roleId"><option value="">—</option>${roleOpts(n.roleId)}</select>`)+fld('Outcomes',`<input data-np="outcomes" data-list value="${esc((n.outcomes||[]).join(', '))}">`)+next;
    }
    if(n.type==='wait'){
      const st=S.get('settings',n.setting);
      f=lab+`<div class="fgrid">${fld('From setting',`<select data-np="setting">${settingOpts(n.setting,['days','hours','min'])}</select>`,1)}
        ${fld('Amount',`<input data-np="amount" data-num value="${esc(st?st.value:n.amount)}" ${st?'disabled':''}>`)}${fld('Unit',`<select data-np="unit" ${st?'disabled':''}>${['hours','days'].map(u=>`<option ${(st?st.unit:n.unit)===u?'selected':''}>${u}</option>`).join('')}</select>`)}</div>`+next;
    }
    if(n.type==='repeat'){
      const e=S.get('settings',n.everySetting),fs=S.get('settings',n.forSetting);
      f=lab+`<div class="fgrid">${fld('Every',`<input data-np="every" data-num value="${esc(e?e.value:n.every)}" ${e?'disabled':''}>`)}${fld('Unit',`<select data-np="everyUnit">${['hours','days'].map(u=>`<option ${n.everyUnit===u?'selected':''}>${u}</option>`).join('')}</select>`)}
        ${fld('Every · from setting',`<select data-np="everySetting">${settingOpts(n.everySetting,['hours','days'])}</select>`,1)}
        ${fld('For',`<input data-np="forAmount" data-num value="${esc(fs?fs.value:n.forAmount)}" ${fs?'disabled':''}>`)}${fld('Unit',`<select data-np="forUnit">${['hours','days'].map(u=>`<option ${n.forUnit===u?'selected':''}>${u}</option>`).join('')}</select>`)}
        ${fld('For · from setting',`<select data-np="forSetting">${settingOpts(n.forSetting,['hours','days'])}</select>`,1)}
        ${fld('Each check needs',`<div class="chkrow">${['photo','video'].map(m=>`<label><input type="checkbox" data-media="${m}" ${(n.media||[]).includes(m)?'checked':''}>${m[0].toUpperCase()+m.slice(1)}</label>`).join('')}</div>`,1)}</div>`+next;
    }
    if(n.type==='child'){
      f=lab+fld('SOP',`<select data-np="sopId"><option value="">—</option>${S.active('sops').filter(x=>x.id!==s.id).map(x=>`<option value="${x.id}" ${n.sopId===x.id?'selected':''}>${esc(x.dept)} · ${esc(x.title)}</option>`).join('')}</select>`)
        +(S.get('sops',n.sopId)?`<a class="btn sm" href="${base}/${n.sopId}">${ic('workflow')}Open child SOP</a>`:'')+`<div class="mt"></div>`+next;
    }
    if(n.type==='action'){
      const it=S.get('items',n.itemId);
      f=lab+`<div class="fgrid">${fld('Item',`<input data-item value="${esc(it?it.name:'')}" placeholder="Search or create" autocomplete="off">`,1)}
        ${fld('Dose / qty',`<input data-np="dose" value="${esc(n.dose||'')}">`)}${fld('Per',`<select data-np="per"><option value="">—</option>${['kg','animal','pen'].map(u=>`<option ${n.per===u?'selected':''}>${u}</option>`).join('')}</select>`)}
        ${fld('Route',`<select data-np="route"><option value="">—</option>${['IM','IV','SQ','Oral','Topical'].map(u=>`<option ${n.route===u?'selected':''}>${u}</option>`).join('')}</select>`)}${pageF}</div>`+next;
    }
    if(n.type==='parallel'){
      f=lab+`<div class="eyebrow">Lanes</div>${(n.next||[]).map((e,i)=>`<div class="row nw mt"><input class="inl" style="flex:1" data-lane="${i}" value="${esc(e.when||'')}"><select class="inl" data-nnext="${i}" style="flex:1">${nodeOpts(s,e.to,x=>x.id!==n.id)}</select></div>`).join('')}
        <button class="btn sm mt" data-a="lane-add">${ic('plus')}Lane</button>`;
    }
    if(n.type==='start'||n.type==='join')f=lab+next;
    if(n.type==='end')f=lab+fld('Outcome',`<select data-np="outcome"><option value="done" ${n.outcome!=='rejected'?'selected':''}>Completed</option><option value="rejected" ${n.outcome==='rejected'?'selected':''}>Rejected</option></select>`);
    const addTypes=['question','evidence','decision','approval','wait','repeat','child','action','parallel','end'];
    return `<div class="hd">${ic(T.icon)}<h3>${T.label}</h3><span class="sp"></span><button class="btn sm gh" data-a="node-sel" data-id="">${ic('x')}</button></div><div class="bd">${f}
      ${n.type!=='end'?`<div class="eyebrow mt">Add after</div><div class="addgrid mt">${addTypes.map(t=>`<button class="btn sm" data-a="node-add" data-t="${t}">${ic(Flow.TYPES[t].icon)}${Flow.TYPES[t].label}</button>`).join('')}</div>`:''}
      ${n.type!=='start'?`<button class="btn sm dngo mt" data-a="node-del">${ic('trash')}Delete step</button>`:''}</div>`;
  }

  W.mountEditor=function(root,id){
    const s=S.get('sops',id); if(!s)return;
    const cv=root.querySelector('[data-canvas]');
    if(cv&&W.scroll&&W.scroll.id===id){cv.scrollLeft=W.scroll.l;cv.scrollTop=W.scroll.t;}
    if(cv)cv.onscroll=()=>{W.scroll={id,l:cv.scrollLeft,t:cv.scrollTop};};
    const insp=root.querySelector('.insp'); if(!insp)return;
    const n=s.nodes.find(x=>x.id===W.sel[id]);
    const commit=(fn,rerender)=>{fn();S.save();if(rerender!==false)App.render();};
    insp.querySelectorAll('[data-sp]').forEach(i=>i.onchange=()=>commit(()=>{s[i.dataset.sp]=i.value.trim()||s[i.dataset.sp];}));
    if(!n)return;
    insp.querySelectorAll('[data-np]').forEach(i=>{
      const k=i.dataset.np;
      const apply=()=>{let v=i.type==='checkbox'?i.checked:i.value;
        if(i.hasAttribute('data-bool'))v=!!v; if(i.hasAttribute('data-num'))v=v===''?'':Number(v);
        if(i.hasAttribute('data-list'))v=v.split(',').map(x=>x.trim()).filter(Boolean);
        if((k==='min'||k==='max')&&n.type==='question')v=v===''?undefined:Number(v);
        if(v===undefined||v==='')delete n[k]; else n[k]=v;
        if(k==='answer'&&(v==='choice'||v==='multi')&&!(n.options||[]).length)n.options=[{v:'yes',l:'Yes'},{v:'no',l:'No'}];
        if(k==='q'){const q=s.nodes.find(x=>x.id===v);if(q&&q.options&&q.options[0])n.value=q.options[0].v;}};
      if(i.tagName==='SELECT'||i.type==='checkbox')i.onchange=()=>commit(apply);
      else{i.oninput=()=>{apply();S.save();App.renderCanvasOnly&&App.renderCanvasOnly();};i.onchange=()=>commit(apply);}
    });
    insp.querySelectorAll('[data-nnext]').forEach(i=>i.onchange=()=>commit(()=>{const ix=+i.dataset.nnext;if(!i.value){n.next.splice(ix,1);return;}if(n.next[ix])n.next[ix].to=i.value;else n.next[ix]={to:i.value};}));
    insp.querySelectorAll('[data-lane]').forEach(i=>i.onchange=()=>commit(()=>{n.next[+i.dataset.lane].when=i.value;const t=s.nodes.find(x=>x.id===n.next[+i.dataset.lane].to);if(t&&t.lane!==undefined)t.lane=i.value;}));
    insp.querySelectorAll('[data-opt]').forEach(i=>i.onchange=()=>commit(()=>{const o=n.options[+i.dataset.opt];o.l=i.value;if(!o.v||o.v.startsWith('opt'))o.v=REG.norm(i.value).replace(/[^a-z0-9]+/g,'_')||o.v;}));
    insp.querySelectorAll('[data-oi]').forEach(i=>i.onchange=()=>commit(()=>{if(i.dataset.oi==='q'){if(!i.value)delete n.onlyIf;else{const q=s.nodes.find(x=>x.id===i.value);n.onlyIf={q:i.value,v:(q.options[0]||{}).v};}}else n.onlyIf.v=i.value;}));
    insp.querySelectorAll('[data-rj]').forEach(i=>i.onchange=()=>commit(()=>{const k=i.dataset.rj;if(k==='op'&&!i.value){delete n.reject;return;}n.reject=n.reject||{op:'=',value:(n.options&&n.options[0]||{}).v||'',reason:'Rejected'};n.reject[k]=i.value;}));
    insp.querySelectorAll('[data-media]').forEach(i=>i.onchange=()=>commit(()=>{const set=new Set(n.media||[]);i.checked?set.add(i.dataset.media):set.delete(i.dataset.media);n.media=[...set];}));
    const it=insp.querySelector('[data-item]');
    if(it){const pop=()=>UI.pop(it,{options:()=>S.active('items').map(x=>({id:x.id,label:x.name,sub:REG.catPath(x.categoryId)})),
        onPick(o){commit(()=>{const x=S.active('items').find(y=>y.name===it.value);n.itemId=x?x.id:'';if(x&&(!n.label||n.label==='New task'))n.label=x.name;});},
        onCreate:t=>{S.snap('Item');const x=S.add('items',{name:t,categoryId:'',unit:'',depts:[s.dept]});n.itemId=x.id;S.save();UI.toast('Created item “'+t+'”');setTimeout(()=>App.render(),0);return {id:x.id,label:t};}});
      it.onfocus=pop;it.oninput=pop;it.onkeydown=e=>UI.popKey(e);it.onblur=()=>setTimeout(UI.closePop,150);}
  };

  Object.assign(A,{
    'node-sel'(el){const id=App.route.params.id;W.sel[id]=el.dataset.id||null;App.render();},
    'node-add'(el){const s=S.get('sops',App.route.params.id);S.snap('Add step');const n=Flow.insertAfter(s,W.sel[s.id],el.dataset.t);S.save();if(n)W.sel[s.id]=n.id;App.render();},
    'node-del'(){const s=S.get('sops',App.route.params.id);const id=W.sel[s.id];S.snap('Delete step');if(!Flow.remove(s,id)){S.undo();UI.toast('This step cannot be deleted');return;}S.save();W.sel[s.id]=null;App.render();UI.toast('Step deleted',()=>{S.undo();App.render();});},
    'opt-add'(){const s=S.get('sops',App.route.params.id);const n=s.nodes.find(x=>x.id===W.sel[s.id]);n.options=n.options||[];n.options.push({v:'opt'+(n.options.length+1),l:'Option '+(n.options.length+1)});S.save();App.render();},
    'opt-del'(el){const s=S.get('sops',App.route.params.id);const n=s.nodes.find(x=>x.id===W.sel[s.id]);n.options.splice(+el.dataset.i,1);S.save();App.render();},
    'lane-add'(){const s=S.get('sops',App.route.params.id);const n=s.nodes.find(x=>x.id===W.sel[s.id]);
      const join=s.nodes.find(x=>x.type==='join')||null;const l=Object.assign({id:Flow.nid(s.nodes)},Flow.defaults('action'),{label:'Lane task',lane:'Lane '+String.fromCharCode(65+n.next.length)});
      l.next=join?[{to:join.id}]:[];s.nodes.push(l);n.next.push({to:l.id,when:l.lane});S.save();App.render();},
    'sop-more'(el){const s=S.get('sops',el.dataset.id);UI.menu(el,[
      {label:'Duplicate SOP',icon:'copy',run(){S.snap('Duplicate');const c=S.add('sops',{dept:s.dept,title:s.title+' copy',category:s.category,nodes:JSON.parse(JSON.stringify(s.nodes)),versions:[]});S.save();location.hash=base+'/'+c.id;}},
      {label:'Archive SOP',icon:'archive',run(){UI.undoable('Archived SOP',()=>{s.status='archived';location.hash=base;});}},
      '-',{label:'Delete SOP',icon:'trash',danger:true,run(){UI.undoable('Deleted SOP',()=>{S.remove('sops',s.id);location.hash=base;});}}]);},
    'sop-publish'(el){
      const s=S.get('sops',el.dataset.id); const iss=Flow.validate(s); const prev=Flow.latest(s);
      const n=t=>s.nodes.filter(x=>x.type===t).length;
      const rows=[['Steps',Flow.stepCount(s.nodes)],['Pages',Flow.pages(s.nodes).length],['Approvals',n('approval')],['Proofs',n('evidence')],['Department',s.dept]].concat(prev?[['Running on v'+prev.v,prev.running||0]]:[]);
      UI.drawer({title:'Publish v'+((prev?prev.v:0)+1),sub:s.title,
        body:`<div class="card"><div class="twrap screen"><table class="ltbl"><tbody>${rows.map(r=>`<tr><td>${esc(r[0])}</td><td class="num"><b>${esc(r[1])}</b></td></tr>`).join('')}</tbody></table></div></div>
          ${iss.length?`<div class="card"><div class="hd"><h3>Issues <span class="cnt">${iss.length}</span></h3></div><div class="bd stack" style="gap:6px">${iss.map(i=>`<button class="btn sm dngo" style="justify-content:flex-start;white-space:normal;text-align:left" data-pubissue="${i.id||''}">${ic('reject')}${esc(i.msg)}</button>`).join('')}</div></div>`:''}`,
        foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-pubgo ${iss.length?'disabled':''}>${ic('check')}Publish</button>`,
        mount(dr){
          dr.querySelectorAll('[data-pubissue]').forEach(b=>b.onclick=()=>{UI.closeDrawer();W.sel[s.id]=b.dataset.pubissue||null;App.render();});
          const go=dr.querySelector('[data-pubgo]'); if(go)go.onclick=()=>{
            UI.closeDrawer();
            const snapN=S.snap('Publish');
            const pins=[];s.nodes.forEach(nd=>['setting','everySetting','forSetting'].forEach(k=>{const st=S.get('settings',nd[k]);if(st)pins.push({id:st.id,name:st.name,value:st.value,unit:st.unit});}));
            s.versions.push({v:(prev?prev.v:0)+1,at:new Date().toISOString(),by:'Ravi Teja',nodes:JSON.parse(JSON.stringify(s.nodes)),running:0,pins});
            S.save(); App.render();
            UI.toast('Published v'+(prev?prev.v+1:1)+(prev&&prev.running?' · '+prev.running+' running stay on v'+prev.v:''),()=>{S.undoTo(snapN);App.render();});
          };
        }});
    },
    'list-ins'(el,e){e.stopPropagation();const s=S.get('sops',App.route.params.id);const id=el.dataset.id;
      UI.menu(el,['question','evidence','decision','approval','wait','repeat','child','action','parallel','end'].map(t=>({label:Flow.TYPES[t].label,icon:Flow.TYPES[t].icon,run(){const snapN=S.snap('Insert');const n=Flow.insertAfter(s,id,t);S.save();if(n)W.sel[s.id]=n.id;App.render();UI.toast('Inserted '+Flow.TYPES[t].label.toLowerCase(),()=>{S.undoTo(snapN);App.render();});}})));},
    'sop-versions'(el){
      const s=S.get('sops',el.dataset.id); const ch=Flow.changed(s);
      const rows=[...s.versions].reverse();
      UI.drawer({title:'Versions',sub:s.title,wide:true,body:`<div class="card"><div class="twrap screen"><table class="ltbl vtbl"><colgroup><col style="width:110px"><col><col style="width:80px"><col style="width:130px"><col style="width:120px"></colgroup><thead><tr><th>Version</th><th>Published</th><th class="num">Steps</th><th>Running</th><th></th></tr></thead><tbody>
        ${ch?`<tr><td><b>Draft</b></td><td class="muted">—</td><td>${Flow.stepCount(s.nodes)}</td><td>${UI.tag('Unpublished','warn')}</td><td></td></tr>`:''}
        ${rows.map((v,i)=>`<tr><td><b>v${v.v}</b> ${i===0?UI.tag('Current','ok'):''}</td><td class="small">${UI.fmtDateTime(v.at)}<div class="muted">${esc(v.by||'')}</div></td><td>${Flow.stepCount(v.nodes)}</td>
          <td>${v.running?UI.tag(v.running+' running','info'):'<span class="muted">0</span>'}${(v.pins||[]).length?`<div class="small muted">${v.pins.map(p=>esc(p.name+' '+p.value+' '+p.unit)).join(' · ')}</div>`:''}</td>
          <td><button class="btn sm" data-restore="${v.v}">${ic('rotate')}Restore</button></td></tr>`).join('')||'<tr><td colspan="5" class="empty">Not published yet</td></tr>'}
        </tbody></table></div></div>`,
        mount(dr){dr.querySelectorAll('[data-restore]').forEach(b=>b.onclick=()=>{const v=s.versions.find(x=>x.v===+b.dataset.restore);UI.closeDrawer();UI.undoable('Draft set to v'+v.v,()=>{s.nodes=JSON.parse(JSON.stringify(v.nodes));W.sel[s.id]=null;App.render();});});}});
    }
  });

  window.Work=W;
})();
