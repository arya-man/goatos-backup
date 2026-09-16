/* Operator view: phone-frame walkthrough of an SOP mimicking the GoatOS Android app.
   API: window.MeshaOperator.render(container, sop, opts) ; .reset(sopId) ; .runMap(sop) */
(function(){
  const RUNS={};
  const E=s=>String(s==null?'':s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const HIDDEN=new Set(['start','end','join']);
  const st=()=>(window.S&&window.S.state)||{};
  const roleName=id=>{const r=(st().roles||[]).find(x=>x.id===id);return r?r.name:(id?String(id).replace(/^role_/,''):'Approver');};
  const setting=id=>{const r=id&&(st().settings||[]).find(x=>x.id===id);return r?Number(r.value):null;};

  function run(sop){
    return RUNS[sop.id]||(RUNS[sop.id]={i:0,ans:{},media:{},appr:{},role:'operator',done:false});
  }

  /* order nodes by DFS from start, following edges in order */
  function order(nodes){
    const m={};nodes.forEach(n=>m[n.id]=n);
    const start=nodes.find(n=>n.type==='start')||nodes[0]; const out=[],seen=new Set();
    (function walk(id){const n=m[id];if(!n||seen.has(id))return;seen.add(id);out.push(n);(n.next||[]).forEach(e=>walk(e.to));})(start&&start.id);
    nodes.forEach(n=>{if(!seen.has(n.id))out.push(n);});
    return out;
  }

  /* group into pages: consecutive same `page`; parallel lanes until join form one page */
  function pages(sop){
    const ns=order(sop.nodes||[]); const out=[]; let cur=null;
    for(let i=0;i<ns.length;i++){
      const n=ns[i];
      if(n.type==='parallel'){
        const lanes={},laneOrder=[]; let j=i+1;
        for(;j<ns.length&&ns[j].type!=='join';j++){const l=ns[j].lane||'Lane '+(laneOrder.length+1);if(!lanes[l]){lanes[l]=[];laneOrder.push(l);}lanes[l].push(ns[j]);}
        out.push({title:n.label||'Parallel',parallel:true,lanes:laneOrder.map(l=>({name:l,nodes:lanes[l]})),nodes:[].concat(...laneOrder.map(l=>lanes[l]))});
        i=j; cur=null; continue;
      }
      if(HIDDEN.has(n.type))continue;
      if(n.page&&cur&&cur.key===n.page){cur.nodes.push(n);continue;}
      cur={key:n.page||null,title:n.page||n.label||'Step',nodes:[n]}; out.push(cur);
      if(!n.page)cur=null;
    }
    return out;
  }

  function visible(n,r){
    if(n.onlyIf){const a=r.ans[n.onlyIf.q];return a!==undefined&&String(a)===String(n.onlyIf.v);}
    return true;
  }
  function rejected(n,r){return window.Flow&&Flow.rejectHit?Flow.rejectHit(n,r.ans[n.id]):false;}
  function nodeDone(n,r){
    switch(n.type){
      case 'question':{const a=r.ans[n.id];if(!n.required)return true;return a!==undefined&&a!==''&&!(Array.isArray(a)&&!a.length);}
      case 'evidence':return (r.media[n.id]||0)>=(n.min||0);
      case 'approval':return !!r.appr[n.id];
      case 'action':case 'child':return !!r.ans[n.id];
      case 'repeat':return (r.ans[n.id]||[]).length>0;
      default:return true;
    }
  }
  function pageState(p,r){
    const ns=p.nodes.filter(n=>visible(n,r));
    if(ns.some(n=>rejected(n,r)))return 'blocked';
    if(ns.some(n=>n.type==='approval'&&!r.appr[n.id]))return ns.every(n=>n.type==='approval'||nodeDone(n,r))?'waiting':'pending';
    return ns.every(n=>nodeDone(n,r))?'done':'pending';
  }

  const pill=(t,tone)=>`<span class="op-pill ${tone||''}">${E(t)}</span>`;
  const lbl=(n,extra)=>`<div class="op-flabel">${E(n.label||'')}${n.required?'<b>*</b>':''}${extra||''}</div>`;

  function fieldHtml(n,r,sop){
    const a=r.ans[n.id]; const rej=rejected(n,r);
    const verdict=rej?`<div class="op-verdict">${pill('Rejected','dng')}<span>${E((n.reject&&n.reject.reason)||'Reject rule hit')}</span></div>`:'';
    const T=(window.Flow&&Flow.TYPES&&Flow.TYPES[n.type])||{label:n.type};
    switch(n.type){
      case 'question':{
        const ans=n.answer||'text'; let inner='';
        if(ans==='scan'){
          inner=`<button class="op-scan ${a?'on':''}" data-op="scan" data-n="${n.id}"><span class="op-ring"></span><span class="op-scan-t">${a?E(a):'Tap to scan'}</span><span class="op-scan-s">Left ear tag</span></button>`;
        }else if(ans==='choice'||ans==='multi'){
          const opts=n.options&&n.options.length?n.options:[{v:'yes',l:'Yes'},{v:'no',l:'No'}];
          const yn=opts.length===2&&opts.every(o=>/^(yes|no)$/i.test(o.v));
          inner=`<div class="op-opts ${yn?'yn':''}">${opts.map(o=>{const on=ans==='multi'?(a||[]).includes(o.v):String(a)===String(o.v);
            return `<button class="op-opt ${on?'on':''}" data-op="${ans==='multi'?'multi':'pick'}" data-n="${n.id}" data-v="${E(o.v)}"><i></i>${E(o.l)}</button>`;}).join('')}</div>`;
          if(n.reject&&n.reject.op==='cannot')inner+=`<button class="op-link" data-op="pick" data-n="${n.id}" data-v="__cannot">Cannot check</button>`;
        }else if(ans==='number'){
          inner=`<div class="op-input"><input inputmode="decimal" data-op="num" data-n="${n.id}" value="${E(a==null?'':a)}" placeholder="${n.min!=null&&n.max!=null?E(n.min+' – '+n.max):'0'}"><span>${E(n.unit||'')}</span></div>`;
          if(n.reject&&n.reject.op==='cannot')inner+=`<button class="op-link" data-op="pick" data-n="${n.id}" data-v="__cannot">Cannot check</button>`;
        }else if(ans==='ref'){
          const L=(window.Flow&&Flow.REFS&&Flow.REFS[n.refColl])||'List';
          const recs=((st()[n.refColl])||[]).filter(x=>x.status!=='archived').slice(0,30);
          inner=`<div class="op-input"><select data-op="sel" data-n="${n.id}"><option value="">Select ${E(L.toLowerCase())}</option>${recs.map(x=>`<option ${a===(x.name||x.id)?'selected':''}>${E(x.name||x.code||x.id)}</option>`).join('')}${recs.length?'':'<option>Sample 1</option><option>Sample 2</option>'}</select></div>`;
        }else if(ans==='date'){
          inner=`<div class="op-input"><input type="date" data-op="txt" data-n="${n.id}" value="${E(a||'')}"></div>`;
        }else{
          inner=`<div class="op-input"><input data-op="txt" data-n="${n.id}" value="${E(a||'')}" placeholder="Type here"></div>`;
        }
        return `<div class="op-card ${rej?'rej':''}">${lbl(n)}${inner}${verdict}</div>`;
      }
      case 'evidence':{
        const c=r.media[n.id]||0,max=n.max||1,media=(n.media||['photo']);
        const tiles=[];for(let k=0;k<Math.max(max,1)&&k<6;k++){const on=k<c;tiles.push(`<button class="op-tile ${on?'on':''}" data-op="media" data-n="${n.id}" data-k="${k}">${on?'<b>✓</b>':'<b>+</b>'}<span>${on?'Captured':media.includes('video')?'Video':'Photo'}</span></button>`);}
        return `<div class="op-card">${lbl(n)}<div class="op-row">${media.map(m=>pill(m,'mut')).join('')}${pill(c+' / '+max,c>=(n.min||0)&&c?'ok':'warn')}</div><div class="op-tiles">${tiles.join('')}</div></div>`;
      }
      case 'approval':{
        const d=r.appr[n.id]; const who=roleName(n.roleId); const outs=n.outcomes&&n.outcomes.length?n.outcomes:['Approve','Send back'];
        const body=d?`<div class="op-row">${pill(d,/back|reject|redo|refer/i.test(d)?'dng':'ok')}<span class="op-mut">by ${E(who)}</span></div>`
          :(r.role==='approver'?`<div class="op-btns">${outs.map((o,i)=>`<button class="op-btn ${i?'ghost':''}" data-op="approve" data-n="${n.id}" data-v="${E(o)}">${E(o)}</button>`).join('')}</div>`
          :`<div class="op-wait"><span class="op-spin"></span>Waiting for approver<span class="op-mut">${E(who)}</span></div>`);
        return `<div class="op-card">${lbl(n)}${body}</div>`;
      }
      case 'wait':{
        const amt=setting(n.amountSetting)||n.amount||1,unit=n.unit||'days';
        return `<div class="op-card">${lbl(n)}<div class="op-timer"><b>${E(amt)}</b><span>${E(unit)} remaining</span></div>${pill('Opens after timer','info')}</div>`;
      }
      case 'repeat':{
        const every=setting(n.everySetting)||n.every||1,eu=n.everyUnit||'hours',fa=setting(n.forSetting)||n.forAmount||1,fu=n.forUnit||'days';
        const hrs=x=>/day/.test(x)?24:/week/.test(x)?168:/min/.test(x)?1/60:1;
        const count=Math.max(1,Math.min(60,Math.floor(fa*hrs(fu)/(every*hrs(eu)))));
        const doneList=r.ans[n.id]||[]; const show=Math.min(count,12);
        const rows=[];for(let k=0;k<show;k++){const on=doneList.includes(k),next=!on&&k===doneList.length;
          rows.push(`<button class="op-occ ${on?'on':next?'next':''}" data-op="occ" data-n="${n.id}" data-k="${k}"><span>#${k+1}</span><span>+${E(k*every)} ${E(eu.replace(/s$/,''))}</span>${on?pill('Done','ok'):next?pill('Due','warn'):pill('Upcoming','mut')}</button>`);}
        return `<div class="op-card">${lbl(n)}<div class="op-row">${pill('Every '+every+' '+eu,'info')}${pill(count+' checks','mut')}</div><div class="op-occs">${rows.join('')}${count>show?`<div class="op-mut">+${count-show} more</div>`:''}</div></div>`;
      }
      case 'decision':{
        let res; try{res=window.Flow&&Flow.evalDecision?Flow.evalDecision(n,r.ans,{prevScans:[]}):undefined;}catch(e){}
        return `<div class="op-card slim"><div class="op-row between"><span class="op-flabel">${E(n.label)}</span>${res===undefined?pill('Pending','mut'):pill(res?'Yes':'No',res?'warn':'ok')}</div></div>`;
      }
      case 'action':case 'child':{
        const on=!!r.ans[n.id]; const sub=n.type==='child'?'Linked SOP':[n.dose&&(n.dose+(n.per?' / '+n.per:'')),n.route].filter(Boolean).join(' · ');
        return `<button class="op-card op-task ${on?'on':''}" data-op="task" data-n="${n.id}"><i></i><span><span class="op-flabel">${E(n.label)}</span>${sub?`<span class="op-mut">${E(sub)}</span>`:''}</span></button>`;
      }
      default:
        return `<div class="op-card slim"><div class="op-row between"><span class="op-flabel">${E(n.label||T.label||n.type)}</span>${pill(T.label||n.type||'Step','mut')}</div></div>`;
    }
  }

  function render(container,sop,opts){
    opts=opts||{}; if(!container)return;
    if(!sop||!Array.isArray(sop.nodes)){container.innerHTML='<div class="op-wrap"><div class="op-phone"><div class="op-screen"><div class="op-head"><div class="op-eyebrow">SOP</div><div class="op-title">No steps</div></div></div></div></div>';return;}
    const r=run(sop); const P=pages(sop); if(r.i>P.length)r.i=P.length;
    const dept=(sop.dept||'Work').toUpperCase();
    const states=P.map(p=>pageState(p,r));
    const summary=r.i>=P.length; const p=P[r.i];
    const doneN=states.filter(s=>s==='done').length; const pct=P.length?Math.round(100*(summary?doneN:r.i)/P.length):0;
    const tone={done:'ok',pending:'warn',blocked:'dng',waiting:'info'};
    const stLabel={done:'Done',pending:'Pending',blocked:'Blocked',waiting:'Waiting'};
    let body,panel;
    if(!P.length){
      body='<div class="op-empty">No operator steps</div>'; panel='';
    }else if(summary){
      const blocked=states.includes('blocked');
      body=`<div class="op-sumhead">${pill(r.done?'Submitted':blocked?'Rejected':doneN===P.length?'Ready':'Incomplete',r.done?'ok':blocked?'dng':doneN===P.length?'ok':'warn')}</div>`+
        P.map((pg,k)=>`<button class="op-sumrow" data-op="goto" data-k="${k}"><span class="op-num">${k+1}</span><span class="op-sumt">${E(pg.title)}</span>${pill(stLabel[states[k]],tone[states[k]])}</button>`).join('');
      panel=`<div class="op-panel"><div class="op-row between"><span class="op-mut">${doneN} of ${P.length} done</span>${blocked?pill('Blocked','dng'):''}</div>
        <div class="op-btns"><button class="op-btn ghost" data-op="back">Back</button><button class="op-btn" data-op="submit" ${r.done||blocked?'disabled':''}>${r.done?'Submitted':'Submit'}</button></div></div>`;
    }else{
      const vis=pg=>pg.filter(n=>visible(n,r));
      body=p.parallel
        ?`<div class="op-lanes">${p.lanes.map(l=>{const ls=vis(l.nodes);const ok=ls.every(n=>nodeDone(n,r));
            return `<div class="op-lane"><div class="op-row between"><span class="op-section">${E(l.name)}</span>${pill(ok?'Done':'Open',ok?'ok':'warn')}</div>${ls.map(n=>fieldHtml(n,r,sop)).join('')}</div>`;}).join('')}</div>`
        :vis(p.nodes).map(n=>fieldHtml(n,r,sop)).join('')||'<div class="op-empty">Nothing to fill</div>';
      const s=states[r.i];
      const cta=s==='blocked'?'Reject and close':s==='waiting'?'Waiting for approver':r.i===P.length-1?'Review':'Next';
      panel=`<div class="op-panel">${s==='blocked'?`<div class="op-verdict big">${pill('Rejected','dng')}<span>Animal fails a reject rule</span></div>`:''}
        <div class="op-btns"><button class="op-btn ghost" data-op="back" ${r.i?'':'disabled'}>Back</button><button class="op-btn ${s==='blocked'?'dng':''}" data-op="${s==='blocked'?'summary':'next'}" ${s==='pending'&&opts.strict?'disabled':''}>${cta}</button></div></div>`;
    }
    const scale=opts.scale||1;
    container.innerHTML=`<div class="op-wrap" style="--op-scale:${scale}"><div class="op-toolbar">
        <div class="op-seg"><button class="${r.role==='operator'?'on':''}" data-op="role" data-v="operator">Operator</button><button class="${r.role==='approver'?'on':''}" data-op="role" data-v="approver">Approver</button></div>
        <button class="op-reset" data-op="reset">Restart</button></div>
      <div class="op-phone"><div class="op-screen">
        <div class="op-status"><span>9:41</span><span>●●● ▮</span></div>
        <div class="op-head">
          <div class="op-hrow"><button class="op-ib" data-op="back" aria-label="Back">‹</button><div><div class="op-eyebrow">${E(dept)}</div><div class="op-title">${E(sop.title||'SOP')}</div></div></div>
          <div class="op-row">${summary?pill('Summary','mut'):p?pill(stLabel[states[r.i]],tone[states[r.i]]):''}${pill((summary?P.length:r.i+1)+' / '+P.length,'mut')}${r.role==='approver'?pill('Approver','info'):''}</div>
          <div class="op-prog"><i style="width:${pct}%"></i></div>
          <div class="op-dots">${P.map((_,k)=>`<button class="op-dot ${states[k]} ${k===r.i?'cur':''}" data-op="goto" data-k="${k}" aria-label="Page ${k+1}"></button>`).join('')}</div>
        </div>
        <div class="op-body">${summary?'':`<div class="op-section">${E(p?p.title:'')}</div>`}${body}</div>
        ${panel}
      </div></div></div>`;

    const rerender=()=>{render(container,sop,opts); if(opts.onChange)opts.onChange(runMap(sop));};
    if(container.__opBound)return; container.__opBound=true;
    container.addEventListener('click',ev=>{
      const b=ev.target.closest('[data-op]'); if(!b||!container.contains(b))return;
      const cs=container.__opSop||sop, R=run(cs), id=b.dataset.n, v=b.dataset.v, op=b.dataset.op;
      const n=(cs.nodes||[]).find(x=>x.id===id); const PP=pages(cs);
      if(op==='num'||op==='txt'||op==='sel')return;
      if(op==='scan')R.ans[id]=R.ans[id]?'':'E'+(100000000000+Math.floor(Math.random()*899999999999));
      else if(op==='pick')R.ans[id]=String(R.ans[id])===v?undefined:v;
      else if(op==='multi'){const a=R.ans[id]||[];R.ans[id]=a.includes(v)?a.filter(x=>x!==v):a.concat(v);}
      else if(op==='media'){const k=+b.dataset.k,c=R.media[id]||0;R.media[id]=k<c?k:k+1;}
      else if(op==='approve')R.appr[id]=v;
      else if(op==='task')R.ans[id]=!R.ans[id];
      else if(op==='occ'){const a=R.ans[id]||[],k=+b.dataset.k;R.ans[id]=a.includes(k)?a.filter(x=>x!==k):a.concat(k);}
      else if(op==='role')R.role=v;
      else if(op==='reset'){delete RUNS[cs.id];}
      else if(op==='back')R.i=Math.max(0,R.i-1);
      else if(op==='next')R.i=Math.min(PP.length,R.i+1);
      else if(op==='summary')R.i=PP.length;
      else if(op==='goto')R.i=+b.dataset.k;
      else if(op==='submit')R.done=true;
      else if(!n)return;
      const o=container.__opOpts||opts;
      render(container,cs,o); if(o.onChange)o.onChange(runMap(cs));
    });
    container.addEventListener('change',ev=>{
      const t=ev.target; if(!t.dataset||!t.dataset.op)return;
      const cs=container.__opSop||sop,R=run(cs);
      R.ans[t.dataset.n]=t.value; const o=container.__opOpts||opts;
      render(container,cs,o); if(o.onChange)o.onChange(runMap(cs));
    });
    void rerender;
  }
  function renderPublic(container,sop,opts){
    if(container){container.__opSop=sop;container.__opOpts=opts||{};}
    render(container,sop,opts);
  }

  /* node id -> 'done' | 'pending' | 'blocked' | 'waiting' for the current try run */
  function runMap(sop){
    const r=RUNS[sop&&sop.id]; const out={}; if(!r||!sop)return out;
    pages(sop).forEach(p=>p.nodes.forEach(n=>{
      if(!visible(n,r))return;
      out[n.id]=rejected(n,r)?'blocked':n.type==='approval'&&!r.appr[n.id]?'waiting':nodeDone(n,r)?'done':'pending';
    }));
    return out;
  }

  window.MeshaOperator={render:renderPublic,runMap,reset(id){delete RUNS[id];},pages};
})();
