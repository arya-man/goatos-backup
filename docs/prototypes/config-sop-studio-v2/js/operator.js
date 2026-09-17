/* Operator view: phone-frame walkthrough of an SOP mimicking the GoatOS Android app.
   API: window.MeshaOperator.render(container, sop, opts) ; .reset(sopId) ; .runMap(sop) */
(function(){
  const RUNS={};
  const E=s=>String(s==null?'':s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const HIDDEN=new Set(['start','end','join']);
  const st=()=>(window.S&&window.S.state)||{};
  const roleName=id=>{const r=(st().roles||[]).find(x=>x.id===id);return r?r.name:'Approver';};
  const approverText=n=>{const ap=window.Flow&&Flow.approvers?Flow.approvers(n):[];return ap.length?ap.map(x=>x.label).join(' → '):roleName(n.roleId);};
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
  /* number answers outside their limits (fixed or read from business settings); a decision on the answer handles it instead */
  let curSop=null;
  function numCheck(n,r){
    if(n.type!=='question'||n.answer!=='number'||!window.Flow||!Flow.limits)return null;
    const a=r.ans[n.id]; if(a===undefined||a===''||a==='__cannot')return null; const x=Number(a);
    const sop=curSop||{nodes:[]}; const L=Flow.limits(n,sop,r.ans);
    const nm=s=>s?' ('+s.name+')':'';
    if(isNaN(x))return {msg:'Enter a number'};
    if(L.min!=null&&x<L.min)return {msg:'Below the minimum '+L.min+nm(L.minSet),handled:handled(n,sop)};
    if(L.max!=null&&x>L.max)return {msg:'Above the maximum '+L.max+nm(L.maxSet),handled:handled(n,sop)};
    return null;
  }
  const handled=(n,sop)=>(sop.nodes||[]).some(d=>d.type==='decision'&&d.q===n.id);
  const numErr=(n,r)=>{const c=numCheck(n,r);return !!(c&&!c.handled);};
  function nodeDone(n,r){
    switch(n.type){
      case 'question':{const a=r.ans[n.id];if(numErr(n,r))return false;if(!n.required)return true;return a!==undefined&&a!==''&&!(Array.isArray(a)&&!a.length);}
      case 'evidence':return (r.media[n.id]||0)>=(n.min||0);
      case 'approval':return !!r.appr[n.id];
      case 'action':case 'child':case 'wait':return !!r.ans[n.id];
      case 'repeat':return (r.ans[n.id]||[]).length>0;
      default:return true;
    }
  }
  function pageState(p,r){
    const ns=p.nodes.filter(n=>visible(n,r));
    if(ns.some(n=>rejected(n,r)))return 'blocked';
    if(ns.some(n=>n.type==='approval'&&!r.appr[n.id]))return ns.every(n=>n.type==='approval'||nodeDone(n,r))?'waiting':'pending';
    if(ns.some(n=>n.type==='wait'&&!r.ans[n.id]))return ns.every(n=>n.type==='wait'||nodeDone(n,r))?'timer':'pending';
    return ns.every(n=>nodeDone(n,r))?'done':'pending';
  }

  const pill=(t,tone)=>`<span class="op-pill ${tone||''}">${E(t)}</span>`;
  const lbl=(n,extra)=>`<div class="op-flabel">${E(n.label||'')}${n.required?'<b>*</b>':''}${extra||''}</div>`;

  /* one pick-list component for every ref question; pens/partitions grouped by park */
  const recLabel=x=>x.displayName||x.name||x.number||x.code||x.id;
  const parkName=id=>{const p=(st().parks||[]).find(x=>x.id===id);return p?p.name:'No park';};
  function parkOf(coll,x){
    if(coll==='pens')return x.parkId;
    if(coll==='partitions'){const pen=(st().pens||[]).find(p=>p.id===x.penId);return pen&&pen.parkId;}
    return undefined;
  }
  function pickList(n,a){
    const coll=n.refColl, L=(window.Flow&&Flow.REFS&&Flow.REFS[coll])||'List';
    const recs=((st()[coll])||[]).filter(x=>x.status!=='archived');
    /* partitions: numeric labels join with a space ("Castro 1"), worded ones with " - " ("Godel 1 - Part 3") */
    const partLabel=x=>{const p=(st().pens||[]).find(y=>y.id===x.penId);const nm=x.name||'';if(!p)return nm;const pn=p.displayName||p.name;return /^\d+$/.test(nm)?pn+' '+nm:pn+' - '+nm;};
    const opt=x=>{const t=coll==='partitions'?partLabel(x):recLabel(x);return `<option value="${E(x.id)}" ${a===x.id?'selected':''}>${E(t)}</option>`;};
    let opts;
    if(!recs.length)return `<div class="op-input op-none">No ${E(L.toLowerCase())} set up yet</div>`;
    else if(coll==='pens'||coll==='partitions'){
      const groups=new Map(); recs.forEach(x=>{const k=parkOf(coll,x)||'';if(!groups.has(k))groups.set(k,[]);groups.get(k).push(x);});
      opts=[...groups].map(([k,xs])=>`<optgroup label="${E(parkName(k))}">${xs.map(opt).join('')}</optgroup>`).join('');
    }else opts=recs.map(opt).join('');
    return `<div class="op-input"><select data-op="sel" data-n="${n.id}" aria-label="${E(n.label||L)}"><option value="">Select ${E(L.toLowerCase())}</option>${opts}</select></div>`;
  }

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
          const L=window.Flow&&Flow.limits?Flow.limits(n,sop,r.ans):{min:n.min,max:n.max};const chk=numCheck(n,r);
          const ph=L.min!=null&&L.max!=null?L.min+' – '+L.max:L.max!=null?'Max '+L.max:L.min!=null?'Min '+L.min:'0';
          inner=`<div class="op-input ${chk?'bad':''}"><input inputmode="decimal" data-op="num" data-n="${n.id}" value="${E(a==null?'':a)}" placeholder="${E(ph)}"><span>${E(n.unit||'')}</span></div>`
            +(L.minSet||L.maxSet?`<div class="op-row op-lims">${[L.minSet&&pill('Min '+L.min+' · '+L.minSet.name,'mut'),L.maxSet&&pill('Max '+L.max+' · '+L.maxSet.name,'mut')].filter(Boolean).join('')}</div>`:'')
            +(chk?`<div class="op-err ${chk.handled?'soft':''}">${E(chk.msg)}</div>`:'');
          if(n.reject&&n.reject.op==='cannot')inner+=`<button class="op-link" data-op="pick" data-n="${n.id}" data-v="__cannot">Cannot check</button>`;
        }else if(ans==='ref'){
          inner=pickList(n,a);
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
        const d=r.appr[n.id]; const who=approverText(n); const outs=n.outcomes&&n.outcomes.length?n.outcomes:['Approve','Send back'];
        const body=d?`<div class="op-row">${pill(d,/back|reject|redo|refer/i.test(d)?'dng':'ok')}<span class="op-mut">by ${E(who)}</span></div>`
          :(r.role==='approver'?`<div class="op-btns">${outs.map((o,i)=>`<button class="op-btn ${i?'ghost':''}" data-op="approve" data-n="${n.id}" data-v="${E(o)}">${E(o)}</button>`).join('')}</div>`
          :`<div class="op-wait"><span class="op-spin"></span>Waiting for approver<span class="op-mut">${E(who)}</span></div>`);
        return `<div class="op-card">${lbl(n)}${body}</div>`;
      }
      case 'wait':{
        const age=n.until==='age',amt=age?n.amount:(setting(n.setting)||setting(n.amountSetting)||n.amount||1),unit=age?'days old':(n.unit||'days'); const over=!!r.ans[n.id];
        return `<div class="op-card">${lbl(n)}${over?`<div class="op-row">${pill(age?'Age reached':'Elapsed','ok')}<span class="op-mut">${age?'Until ':''}${E(amt)} ${E(unit)}</span></div>`
          :`<div class="op-timer">${age?'<span>Until</span>':''}<b>${E(amt)}</b><span>${E(unit)}${age?'':' remaining'}</span></div><button class="op-link" data-op="elapse" data-n="${n.id}">Skip wait</button>`}</div>`;
      }
      case 'repeat':{
        const every=setting(n.everySetting)||n.every||1,eu=n.everyUnit||'hours',fa=setting(n.forSetting)||n.forAmount||1,fu=n.forUnit||'days';
        const hrs=x=>/day/.test(x)?24:/week/.test(x)?168:/min/.test(x)?1/60:1;
        const times=n.times||[];
        const count=times.length?Math.min(60,times.length*Math.max(1,Math.round(fa*hrs(fu)/24))):Math.max(1,Math.min(60,Math.floor(fa*hrs(fu)/(every*hrs(eu)))));
        const doneList=r.ans[n.id]||[]; const show=Math.min(count,12);
        const when=k=>times.length?'Day '+(Math.floor(k/times.length)+1)+' · '+times[k%times.length]:'+'+(k*every)+' '+eu.replace(/s$/,'');
        const rows=[];for(let k=0;k<show;k++){const on=doneList.includes(k),next=!on&&k===doneList.length;
          rows.push(`<button class="op-occ ${on?'on':next?'next':''}" data-op="occ" data-n="${n.id}" data-k="${k}"><span>#${k+1}</span><span>${E(when(k))}</span>${on?pill('Done','ok'):next?pill('Due','warn'):pill('Upcoming','mut')}</button>`);}
        return `<div class="op-card">${lbl(n)}<div class="op-row">${pill(times.length?times.length+' times a day':'Every '+every+' '+eu,'info')}${pill(count+' checks','mut')}</div><div class="op-occs">${rows.join('')}${count>show?`<div class="op-mut">+${count-show} more</div>`:''}</div></div>`;
      }
      case 'decision':{
        let res; try{res=window.Flow&&Flow.evalDecision?Flow.evalDecision(n,r.ans,{prevScans:[]}):undefined;}catch(e){}
        return `<div class="op-card slim"><div class="op-row between"><span class="op-flabel">${E(Flow.label?Flow.label(n):n.label)}</span>${res===undefined?pill('Pending','mut'):pill(res?'Yes':'No',res?'warn':'ok')}</div></div>`;
      }
      case 'action':case 'child':{
        if(n.link){const on=!!r.ans[n.id]; const steps=n.linkSteps||(/vaccin/i.test(n.link)?['Calendar','Sheds','Scan']:[]);
          return `<div class="op-card op-linkcard ${on?'on':''}"><div class="op-row between"><span class="op-flabel">${E(n.label)}</span>${pill(on?'Done':'Module','mut')}</div>
            ${steps.length?`<div class="op-chain">${steps.map((x,k)=>`${k?'<i>›</i>':''}<span>${E(x)}</span>`).join('')}</div>`:''}
            <div class="op-btns"><a class="op-btn ghost op-open" href="${E(n.link)}">Open ${E(n.linkLabel||'module')}</a><button class="op-btn ${on?'ghost':''}" data-op="task" data-n="${n.id}">${on?'Undo':'Mark done'}</button></div></div>`;}
        const on=!!r.ans[n.id]; const sub=n.type==='child'?'Linked SOP':[n.dose&&(n.dose+(n.per?' / '+n.per:'')),n.route].filter(Boolean).join(' · ');
        return `<button class="op-card op-task ${on?'on':''}" data-op="task" data-n="${n.id}"><i></i><span><span class="op-flabel">${E(n.label)}</span>${sub?`<span class="op-mut">${E(sub)}</span>`:''}</span></button>`;
      }
      default:
        return `<div class="op-card slim"><div class="op-row between"><span class="op-flabel">${E(n.label||T.label||n.type)}</span>${pill(T.label||n.type||'Step','mut')}</div></div>`;
    }
  }

  /* skip the caps page title when the page's only field already says the same thing */
  const norm=t=>String(t||'').trim().toLowerCase();
  const sameTitle=p=>!p.parallel&&p.nodes.length===1&&norm(p.nodes[0].label)===norm(p.title);
  function render(container,sop,opts){
    opts=opts||{}; if(!container)return;
    if(!sop||!Array.isArray(sop.nodes)){container.innerHTML='<div class="op-wrap"><div class="op-phone"><div class="op-screen"><div class="op-head"><div class="op-eyebrow">SOP</div><div class="op-title">No steps</div></div></div></div></div>';return;}
    curSop=sop; const r=run(sop); const P=pages(sop); if(r.i>P.length)r.i=P.length;
    const dept=(sop.dept||'Work').toUpperCase();
    const states=P.map(p=>pageState(p,r));
    const summary=r.i>=P.length; const p=P[r.i];
    const doneN=states.filter(s=>s==='done').length; const pct=P.length?Math.round(100*(summary?doneN:r.i)/P.length):0;
    const tone={done:'ok',pending:'warn',blocked:'dng',waiting:'info',timer:'info'};
    const stLabel={done:'Done',pending:'Pending',blocked:'Blocked',waiting:'Awaiting approval',timer:'Waiting'};
    const reach=(()=>{let k=0;while(k<P.length&&states[k]==='done')k++;return k;})();
    let body,panel;
    if(!P.length){
      body='<div class="op-empty">No operator steps</div>'; panel='';
    }else if(summary){
      const blocked=states.includes('blocked');
      body=`<div class="op-sumhead">${pill(r.done?'Submitted':blocked?'Rejected':doneN===P.length?'Ready':'Incomplete',r.done?'ok':blocked?'dng':doneN===P.length?'ok':'warn')}</div>`+
        P.map((pg,k)=>`<button class="op-sumrow" data-op="goto" data-k="${k}"><span class="op-num">${k+1}</span><span class="op-sumt">${E(pg.title)}</span>${pill(stLabel[states[k]],tone[states[k]])}</button>`).join('');
      panel=`<div class="op-panel"><div class="op-row between"><span class="op-mut">${doneN} of ${P.length} done</span>${blocked?pill('Blocked','dng'):''}</div>
        <div class="op-btns"><button class="op-btn ghost" data-op="back">Back</button><button class="op-btn" data-op="submit" ${r.done||blocked||doneN<P.length?'disabled':''}>${r.done?'Submitted':doneN<P.length?'Incomplete':'Submit'}</button></div></div>`;
    }else{
      const vis=pg=>pg.filter(n=>visible(n,r));
      body=p.parallel
        ?`<div class="op-lanes">${p.lanes.map(l=>{const ls=vis(l.nodes);const ok=ls.every(n=>nodeDone(n,r));
            return `<div class="op-lane"><div class="op-row between"><span class="op-section">${E(l.name)}</span>${pill(ok?'Done':'Open',ok?'ok':'warn')}</div>${ls.map(n=>fieldHtml(n,r,sop)).join('')}</div>`;}).join('')}</div>`
        :vis(p.nodes).map(n=>fieldHtml(n,r,sop)).join('')||'<div class="op-empty">Nothing to fill</div>';
      const s=states[r.i];
      const stop=s==='waiting'||s==='timer'||s==='pending';
      const cta=s==='blocked'?'Reject and close':s==='waiting'?'Awaiting approval':s==='timer'?'Waiting':s==='pending'?'Incomplete':r.i===P.length-1?'Review':'Next';
      panel=`<div class="op-panel">${s==='blocked'?`<div class="op-verdict big">${pill('Rejected','dng')}<span>Animal fails a reject rule</span></div>`:''}
        <div class="op-btns"><button class="op-btn ghost" data-op="back" ${r.i?'':'disabled'}>Back</button><button class="op-btn ${s==='blocked'?'dng':''} ${stop?'held':''}" data-op="${s==='blocked'?'summary':'next'}" ${stop?'disabled':''}>${stop?'<span class="op-lock"></span>':''}${cta}</button></div></div>`;
    }
    const scale=opts.scale||1;
    container.innerHTML=`<div class="op-wrap" style="--op-scale:${scale}"><div class="op-toolbar">
        <div class="op-seg"><button class="${r.role==='operator'?'on':''}" data-op="role" data-v="operator">Operator</button><button class="${r.role==='approver'?'on':''}" data-op="role" data-v="approver">Approver</button></div>
        <button class="op-reset" data-op="reset">Restart</button></div>
      <div class="op-phone"><div class="op-screen">
        <div class="op-head">
          <div class="op-hrow"><button class="op-ib" data-op="back" aria-label="Back">‹</button><div><div class="op-eyebrow">${E(dept)}</div><div class="op-title">${E(sop.title||'SOP')}</div></div></div>
          <div class="op-row">${summary?'':p?pill(stLabel[states[r.i]],tone[states[r.i]]):''}${pill(summary?'Summary of '+P.length:'Page '+(r.i+1)+' of '+P.length,'mut')}${r.role==='approver'?pill('Approver','info'):''}</div>
          <div class="op-prog"><i style="width:${pct}%"></i></div>
          <div class="op-dots">${P.map((_,k)=>`<button class="op-dot ${states[k]==='done'&&(k<r.i||summary)?'done':states[k]==='blocked'?'blocked':k<r.i?'pending':''} ${k===r.i?'cur':''}" data-op="goto" data-k="${k}" aria-label="Page ${k+1}" ${k>reach?'disabled':''}></button>`).join('')}</div>
        </div>
        <div class="op-body">${summary||!p||sameTitle(p)?'':`<div class="op-section">${E(p.title)}</div>`}${body}</div>
        ${panel}
      </div></div></div>`;

    const rerender=()=>{render(container,sop,opts); if(opts.onChange)opts.onChange(runMap(sop));};
    if(container.__opBound)return; container.__opBound=true;
    container.addEventListener('click',ev=>{
      const b=ev.target.closest('[data-op]'); if(!b||!container.contains(b))return;
      const cs=container.__opSop||sop, R=run(cs), id=b.dataset.n, v=b.dataset.v, op=b.dataset.op; curSop=cs;
      const n=(cs.nodes||[]).find(x=>x.id===id); const PP=pages(cs);
      if(op==='num'||op==='txt'||op==='sel')return;
      if(op==='scan')R.ans[id]=R.ans[id]?'':'E'+(100000000000+Math.floor(Math.random()*899999999999));
      else if(op==='pick')R.ans[id]=String(R.ans[id])===v?undefined:v;
      else if(op==='multi'){const a=R.ans[id]||[];R.ans[id]=a.includes(v)?a.filter(x=>x!==v):a.concat(v);}
      else if(op==='media'){const k=+b.dataset.k,c=R.media[id]||0;R.media[id]=k<c?k:k+1;}
      else if(op==='approve')R.appr[id]=v;
      else if(op==='task')R.ans[id]=!R.ans[id];
      else if(op==='occ'){const a=R.ans[id]||[],k=+b.dataset.k;R.ans[id]=a.includes(k)?a.filter(x=>x!==k):a.concat(k);}
      else if(op==='role'){R.role=v;if(v==='approver'){const k=PP.findIndex(pg=>pg.nodes.some(x=>x.type==='approval'&&!R.appr[x.id]&&visible(x,R)));if(k>=0)R.i=k;}}
      else if(op==='reset'){delete RUNS[cs.id];}
      else if(op==='back')R.i=Math.max(0,R.i-1);
      else if(op==='next'){const s0=PP[R.i]&&pageState(PP[R.i],R);if(s0&&s0!=='done')return;R.i=Math.min(PP.length,R.i+1);}
      else if(op==='elapse')R.ans[id]=true;
      else if(op==='summary')R.i=PP.length;
      else if(op==='goto'){const k=+b.dataset.k;let ok=0;while(ok<PP.length&&pageState(PP[ok],R)==='done')ok++;if(k>ok)return;R.i=k;}
      else if(op==='submit'){if(!PP.every(pg=>pageState(pg,R)==='done'))return;R.done=true;}
      else if(!n)return;
      const o=container.__opOpts||opts;
      render(container,cs,o); if(o.onChange)o.onChange(runMap(cs));
    });
    container.addEventListener('change',ev=>{
      const t=ev.target; if(!t.dataset||!t.dataset.op)return;
      const cs=container.__opSop||sop,R=run(cs); curSop=cs;
      R.ans[t.dataset.n]=t.value; const o=container.__opOpts||opts;
      setTimeout(()=>{render(container,cs,o); if(o.onChange)o.onChange(runMap(cs));},0);
    });
    void rerender;
  }
  function renderPublic(container,sop,opts){
    if(container){container.__opSop=sop;container.__opOpts=opts||{};}
    render(container,sop,opts);
  }

  /* node id -> 'done' | 'pending' | 'blocked' | 'waiting' for the current try run */
  function runMap(sop){
    const r=RUNS[sop&&sop.id]; const out={}; if(!r||!sop)return out; curSop=sop;
    pages(sop).forEach(p=>p.nodes.forEach(n=>{
      if(!visible(n,r))return;
      out[n.id]=rejected(n,r)?'blocked':(n.type==='approval'&&!r.appr[n.id])||(n.type==='wait'&&!r.ans[n.id])?'waiting':nodeDone(n,r)?'done':'pending';
    }));
    return out;
  }

  window.MeshaOperator={render:renderPublic,runMap,reset(id){delete RUNS[id];},pages};
})();
