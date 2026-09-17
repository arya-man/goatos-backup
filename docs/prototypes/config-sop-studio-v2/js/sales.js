/* Items and settings / Sales: sale price rules, sale eligibility (shared with Weighing), valuation rates, preview.
   Every register row is one dated version of a scope; the latest version on or before today is current. */
(function(){
  const X={};
  const DIMS={salePrices:['speciesId','breedId','sexId','stageId'],saleMinWeights:['speciesId','breedId','sexId'],valuationRates:['name'],salePolicy:[]};
  const pad=n=>String(n).padStart(2,'0');
  const today=()=>{const d=new Date();return d.getFullYear()+'-'+pad(d.getMonth()+1)+'-'+pad(d.getDate());};
  const fmt=iso=>iso?UI.fmtDate(iso+'T00:00:00'):'';
  const inr=n=>'₹'+Number(n).toLocaleString('en-IN',{maximumFractionDigits:2});
  const any=(coll,id)=>id?esc(REG.labelById(coll,id)):'<span class="muted">Any</span>';
  const scopeKey=(coll,r)=>DIMS[coll].map(d=>REG.norm(r[d]||'')).join('|');
  const spec=(coll,r)=>DIMS[coll].slice(1).filter(d=>r[d]).length;

  /* version state for every active row of a collection */
  function states(coll){
    const t=today(),groups={},out={};
    S.active(coll).forEach(r=>(groups[scopeKey(coll,r)]=groups[scopeKey(coll,r)]||[]).push(r));
    Object.values(groups).forEach(g=>{
      g.sort((a,b)=>(a.from||'').localeCompare(b.from||''));
      const due=g.filter(r=>(r.from||'')<=t),cur=due[due.length-1];
      const dup={};g.forEach(r=>{dup[r.from||'']=(dup[r.from||'']||0)+1;});
      g.forEach(r=>{out[r.id]={state:r===cur?'current':(r.from||'')>t?'scheduled':'superseded',dup:dup[r.from||'']>1,versions:g};});
    });
    return out;
  }
  const current=coll=>{const st=states(coll);return S.active(coll).filter(r=>st[r.id].state==='current');};
  const policy=()=>{const c=current('salePolicy');return c[c.length-1]||{toleranceG:0,reportKg:'',stageIds:[]};};

  /* most specific current rule wins; equal specificity from different scopes is a conflict */
  function match(coll,a){
    const hits=current(coll).filter(r=>r.speciesId===a.speciesId&&DIMS[coll].slice(1).every(d=>!r[d]||r[d]===a[d]))
      .sort((x,y)=>spec(coll,y)-spec(coll,x));
    const top=hits[0]||null;
    return {rule:top,conflict:top?hits.filter(h=>h!==top&&spec(coll,h)===spec(coll,top)):[]};
  }
  function conflicts(coll){
    const cur=current(coll),out={};
    cur.forEach(a=>cur.forEach(b=>{if(a===b||a.speciesId!==b.speciesId||spec(coll,a)!==spec(coll,b))return;
      if(DIMS[coll].slice(1).every(d=>!a[d]||!b[d]||a[d]===b[d]))(out[a.id]=out[a.id]||[]).push(b);}));
    return out;
  }
  function appliesTo(coll){
    const n={};S.active('animals').forEach(a=>{const m=match(coll,a);if(m.rule)n[m.rule.id]=(n[m.rule.id]||0)+1;});return n;
  }
  function weighState(a,w){
    const mw=match('saleMinWeights',a).rule,p=policy(),tol=(+p.toleranceG||0)/1000;
    if(!mw||w===''||w==null||isNaN(w))return {mw,state:'',tol};
    return {mw,tol,state:+w>=mw.minKg?'ready':+w>=mw.minKg-tol?'margin':'below'};
  }

  const usedBy=list=>`<div class="usedby"><span class="muted small">Used by</span>${list.map(([l,h])=>`<a class="chip" href="${h}">${esc(l)}</a>`).join('')}</div>`;
  const U={w:['Weighing','#/weighing/analytics'],o:['Weighing SOP','#/weighing/sops'],d:['Sales deals','#/sales/sold'],f:['Farm value','#/sales/farm-value']};
  const stateTag=(s,extra)=>(s.dup?UI.tag('Duplicate','dng')+' ':'')+(extra&&extra.length?UI.tag('Conflict','dng')+' ':'')+(s.state==='current'?UI.tag('Current','ok'):s.state==='scheduled'?UI.tag('Scheduled','info'):UI.tag('Superseded','mut'));
  const sortScope=coll=>(a,b)=>DIMS[coll].map(d=>{const c=d==='name'?null:d.replace(/Id$/,'')==='sex'?'sexes':d.replace(/Id$/,'')==='species'?'species':d.replace(/Id$/,'')+'s';
      return (c?(a[d]?REG.labelById(c,a[d]):''):a[d]||'').localeCompare(c?(b[d]?REG.labelById(c,b[d]):''):b[d]||'');}).find(x=>x)||(a.from||'').localeCompare(b.from||'');
  function list(k,opts){List.opts[k]=opts;return List.html(k,opts);}
  const gridBtn=k=>`<button class="btn sm" data-a="sl-grid" data-reg="${k}">${ic('sheet')}Edit in grid</button>`;
  const histBtn=k=>`<button class="btn sm" data-a="sl-hist" data-reg="${k}">${ic('rotate')}History</button>`;

  /* ---------- panels ---------- */
  X.prices=function(){
    const st=states('salePrices'),cf=conflicts('salePrices'),n=appliesTo('salePrices');
    const bad=S.active('salePrices').filter(r=>st[r.id].dup||cf[r.id]).length;
    return usedBy([U.d,U.f])
      +(bad?`<div class="slwarn"><b>${bad}</b>rule${bad===1?'':'s'} in conflict or duplicated</div>`:'')
      +list('salePrices',{eyebrow:'Sales',title:'Sale price rules',filters:['species','basis'],addLabel:'Add rule',sort:sortScope('salePrices'),
        defaults:()=>({from:today(),basis:REG.enumLabel('per_kg')}),onOpen:id=>versionDrawer('salePrices',id),extraBtns:histBtn('salePrices')+gridBtn('salePrices'),
        columns:[{label:'Species',html:r=>`<b>${esc(REG.labelById('species',r.speciesId))}</b>`},{label:'Breed',w:120,html:r=>any('breeds',r.breedId)},
          {label:'Sex',w:70,html:r=>any('sexes',r.sexId)},{label:'Stage',w:150,html:r=>any('stages',r.stageId)},
{label:'Price',w:120,html:r=>`<span class="num">${inr(r.price)}</span><span class="muted small">${r.basis==='per_kg'?' /kg':' /animal'}</span>`},
          {label:'Animals',w:90,html:r=>st[r.id]&&st[r.id].state==='current'?`<span class="num">${n[r.id]||0}</span>`:'<span class="muted">—</span>'},
          {label:'Version',html:r=>(st[r.id]?stateTag(st[r.id],cf[r.id]):UI.tag('Archived','mut'))+`<div class="muted small">From ${esc(fmt(r.from))}</div>`}]})
      +preview();
  };

  X.eligibility=function(){
    const p=policy(),st=states('saleMinWeights'),n=appliesTo('saleMinWeights'),cf=conflicts('saleMinWeights');
    const stages=(p.stageIds||[]).map(id=>S.get('stages',id)).filter(Boolean);
    const elig=S.active('animals').filter(a=>(p.stageIds||[]).includes(a.stageId));
    const c={ready:0,margin:0,below:0};elig.forEach(a=>{const w=weighState(a,a.weight);if(w.state)c[w.state]++;});
    return usedBy([U.w,U.o,U.d])
      +`<section class="card"><div class="hd lhd"><div class="ttl"><div class="eyebrow">Sales</div><h3>Sale eligibility</h3></div><span class="sp"></span>${histBtn('salePolicy')}<button class="btn sm p" data-a="sl-policy">${ic('edit-3')}Edit</button></div>
        <div class="slpol">
          <div class="kv"><div class="lab">Weight error margin</div><div class="val"><span class="num">${esc(p.toleranceG)}</span> g</div></div>
          <div class="kv"><div class="lab">Reporting threshold</div><div class="val"><span class="num">${esc(p.reportKg)}</span> kg</div></div>
          <div class="kv"><div class="lab">Effective from</div><div class="val">${p.from?esc(fmt(p.from)):'<span class="muted">—</span>'}</div></div>
          <div class="kv wide"><div class="lab">Eligible stages</div><div class="chips">${stages.length?stages.map(s=>UI.tag(s.name+' · '+REG.labelById('species',s.speciesId),'teal')).join(''):'<span class="muted">None</span>'}</div></div>
        </div>
        <div class="grid g3 slkpi">${[['Eligible · sale-ready',c.ready],['Eligible · borderline',c.margin],['Eligible · below',c.below]].map(([l,v])=>`<div class="kpi"><div class="lab">${l}</div><div class="val">${v}</div></div>`).join('')}</div>
      </section>`
      +`<div class="mt">${list('saleMinWeights',{eyebrow:'Sale eligibility',title:'Minimum sale weight',filters:['species'],addLabel:'Add override',sort:sortScope('saleMinWeights'),
        defaults:()=>({from:today()}),onOpen:id=>versionDrawer('saleMinWeights',id),extraBtns:histBtn('saleMinWeights')+gridBtn('saleMinWeights'),
        columns:[{label:'Species',html:r=>`<b>${esc(REG.labelById('species',r.speciesId))}</b>`},{label:'Breed',html:r=>any('breeds',r.breedId)},{label:'Sex',w:80,html:r=>any('sexes',r.sexId)},
          {label:'Minimum',w:110,html:r=>`<span class="num">${esc(r.minKg)}</span> kg`},{label:'Effective from',w:120,html:r=>r.from?esc(fmt(r.from)):'<span class="muted">—</span>'},
          {label:'Applies to',w:100,html:r=>st[r.id]&&st[r.id].state==='current'?`<span class="num">${n[r.id]||0}</span> <span class="muted small">animals</span>`:'<span class="muted">—</span>'},
          {label:'Version',html:r=>st[r.id]?stateTag(st[r.id],cf[r.id]):UI.tag('Archived','mut')}]})}</div>`
      +preview();
  };

  X.valuation=function(){
    const st=states('valuationRates');
    return usedBy([U.f])
      +list('valuationRates',{eyebrow:'Sales',title:'Valuation rates',addLabel:'Add group',sort:(a,b)=>0,defaults:()=>({from:today()}),
        onOpen:id=>versionDrawer('valuationRates',id),extraBtns:histBtn('valuationRates')+gridBtn('valuationRates'),
        columns:[{label:'Group',html:r=>`<b>${esc(r.name)}</b>`},{label:'Stages',low:1,html:r=>esc(r.stages||'')},{label:'Rate',w:120,html:r=>`<span class="num">${inr(r.rate)}</span><span class="muted small"> /kg</span>`},
          {label:'Assumed weight',w:150,html:r=>r.assumedKg===''||r.assumedKg==null?'<span class="muted">Measured average</span>':`<span class="num">${esc(r.assumedKg)}</span> kg`},
          {label:'Value per animal',w:140,low:1,html:r=>r.assumedKg===''||r.assumedKg==null?'<span class="muted">—</span>':`<span class="num">${inr(r.rate*r.assumedKg)}</span>`},
          {label:'Effective from',w:120,html:r=>r.from?esc(fmt(r.from)):'<span class="muted">—</span>'},{label:'Version',html:r=>st[r.id]?stateTag(st[r.id]):UI.tag('Archived','mut')}]});
  };
  X.count={salePrices:()=>S.active('salePrices').length,saleEligibility:()=>S.active('saleMinWeights').length,valuationRates:()=>S.active('valuationRates').length};

  /* ---------- preview ---------- */
  let PV={animal:'',speciesId:'sp_goat',breedId:'',sexId:'',stageId:'',weight:''};
  const opt=(list,sel,blank)=>(blank!=null?`<option value="">${esc(blank)}</option>`:'')+list.map(x=>`<option value="${esc(x.id)}" ${x.id===sel?'selected':''}>${esc(x.label)}</option>`).join('');
  function pvFields(){
    const sp=PV.speciesId;
    return `<div class="fld"><label>Animal</label><select data-pv="animal">${opt(S.active('animals').map(a=>({id:a.id,label:a.rfid.slice(-6)+' · '+REG.labelById('breeds',a.breedId)+' · '+REG.labelById('stages',a.stageId)})),PV.animal,'Enter manually')}</select></div>
      <div class="fld"><label>Species</label><select data-pv="speciesId">${opt(S.active('species').map(x=>({id:x.id,label:x.name})),sp)}</select></div>
      <div class="fld"><label>Breed</label><select data-pv="breedId">${opt(S.active('breeds').filter(x=>x.speciesId===sp).map(x=>({id:x.id,label:x.name})),PV.breedId,'—')}</select></div>
      <div class="fld"><label>Sex</label><select data-pv="sexId">${opt(S.active('sexes').map(x=>({id:x.id,label:x.name})),PV.sexId,'—')}</select></div>
      <div class="fld"><label>Stage</label><select data-pv="stageId">${opt(S.active('stages').filter(x=>x.speciesId===sp).map(x=>({id:x.id,label:x.name})),PV.stageId,'—')}</select></div>
      <div class="fld"><label>Weight (kg)</label><input data-pv="weight" inputmode="decimal" value="${esc(PV.weight)}"></div>`;
  }
  const scopeText=(coll,r)=>[r.breedId?REG.labelById('breeds',r.breedId):'Any breed',r.sexId?REG.labelById('sexes',r.sexId):'Any sex'].concat(coll==='salePrices'?[r.stageId?REG.labelById('stages',r.stageId):'Any stage']:[]).join(' · ');
  function pvOut(){
    const a=PV,w=a.weight===''?'':Number(a.weight),p=policy();
    const pm=match('salePrices',a),ws=weighState(a,w),stageOk=(p.stageIds||[]).includes(a.stageId);
    const row=(l,v)=>`<div class="pvr"><span class="muted">${l}</span><span>${v}</span></div>`;
    const est=pm.rule&&(pm.rule.basis==='per_animal'?inr(pm.rule.price):w===''||isNaN(w)?'<span class="muted">Weight needed</span>':inr(pm.rule.price*w));
    return row('Price rule',pm.rule?`<b>${esc(REG.labelById('species',pm.rule.speciesId)+' · '+scopeText('salePrices',pm.rule))}</b> · ${inr(pm.rule.price)}${pm.rule.basis==='per_kg'?'/kg':'/animal'}${pm.conflict.length?' '+UI.tag('Conflict','dng'):''}`:UI.tag('No rule','warn'))
      +row('Minimum weight',ws.mw?`<b>${esc(ws.mw.minKg)} kg</b> · ${esc(scopeText('saleMinWeights',ws.mw))}`:UI.tag('Not set','warn'))
      +row('Eligible stage',a.stageId?(stageOk?UI.tag('Yes','ok'):UI.tag('No','mut')):'<span class="muted">—</span>')
      +row('Within error margin',ws.state==='margin'?UI.tag('Yes · '+Math.round((ws.mw.minKg-w)*1000)+' g under','warn'):ws.state?`<span class="muted">No</span>`:'<span class="muted">—</span>')
      +row('Sale eligible',!ws.state||!a.stageId?'<span class="muted">—</span>':stageOk&&ws.state==='ready'?UI.tag('Yes','ok'):stageOk&&ws.state==='margin'?UI.tag('Borderline','warn'):UI.tag('No','dng'))
      +row('Estimated price',est?`<b class="num">${est}</b>`:'<span class="muted">—</span>');
  }
  function preview(){
    return `<section class="card mt" data-pvcard><div class="hd lhd"><div class="ttl"><div class="eyebrow">Preview</div><h3>Price and eligibility</h3></div></div>
      <div class="pvgrid"><div class="fgrid pvf" data-pvf>${pvFields()}</div><div class="pvout" data-pvout>${pvOut()}</div></div></section>`;
  }
  document.addEventListener('change',e=>{const t=e.target;if(!(t.matches&&t.matches('[data-pv]')))return;const k=t.dataset.pv;const card=t.closest('[data-pvcard]');
    if(k==='animal'){const a=S.get('animals',t.value);PV.animal=t.value;if(a)Object.assign(PV,{speciesId:a.speciesId,breedId:a.breedId||'',sexId:a.sexId||'',stageId:a.stageId||'',weight:a.weight==null?'':String(a.weight)});}
    else{PV[k]=t.value;PV.animal='';if(k==='speciesId'){PV.breedId='';PV.stageId='';}}
    card.querySelector('[data-pvf]').innerHTML=pvFields();card.querySelector('[data-pvout]').innerHTML=pvOut();});
  document.addEventListener('input',e=>{const t=e.target;if(!(t.matches&&t.matches('[data-pv="weight"]')))return;PV.weight=t.value;PV.animal='';
    const card=t.closest('[data-pvcard]');const s=card.querySelector('[data-pv="animal"]');if(s)s.value='';card.querySelector('[data-pvout]').innerHTML=pvOut();});

  /* ---------- versioned edit ---------- */
  const FIELDS={salePrices:[['basis','Price basis','enum',['per_kg','per_animal']],['price','Price (INR)','num']],saleMinWeights:[['minKg','Minimum (kg)','num']],
    valuationRates:[['rate','Rate (INR/kg)','num'],['assumedKg','Assumed weight (kg)','num',null,1],['stages','Stages','text']]};
  function scopeLine(coll,r){return coll==='valuationRates'?r.name:REG.labelById('species',r.speciesId)+' · '+scopeText(coll,r);}
  function histTable(coll,rows,st){
    const f=FIELDS[coll]||[];
    return `<div class="sltbl-wrap"><table class="sltbl"><thead><tr><th>Effective from</th>${coll==='salePolicy'?'<th>Margin</th><th>Reporting</th><th>Stages</th>':f.map(x=>`<th>${esc(x[1])}</th>`).join('')}<th>Status</th></tr></thead><tbody>${
      rows.slice().reverse().map(r=>`<tr><td>${r.from?esc(fmt(r.from)):'<span class="muted">—</span>'}</td>${coll==='salePolicy'?`<td class="num">${esc(r.toleranceG)} g</td><td class="num">${esc(r.reportKg)} kg</td><td>${(r.stageIds||[]).length}</td>`
        :f.map(x=>`<td>${x[2]==='enum'?esc(REG.enumLabel(r[x[0]])):esc(r[x[0]]===''||r[x[0]]==null?'—':r[x[0]])}</td>`).join('')}<td>${st[r.id]?stateTag(st[r.id]):''}</td></tr>`).join('')}</tbody></table></div>`;
  }
  function versionDrawer(coll,id){
    const r=S.get(coll,id);if(!r)return;const st=states(coll),s=st[r.id],f=FIELDS[coll];
    const v={from:today()};f.forEach(x=>v[x[0]]=r[x[0]]==null?'':String(r[x[0]]));
    UI.drawer({title:REG.R[coll].one,sub:scopeLine(coll,r),cls:'idr',wide:1,
      body:`<div class="fgrid">${f.map(([k,l,t,o])=>`<div class="fld"><label>${esc(l)}</label>${t==='enum'?`<select data-vd="${k}">${o.map(x=>`<option value="${x}" ${v[k]===x?'selected':''}>${esc(REG.enumLabel(x))}</option>`).join('')}</select>`:`<input data-vd="${k}" value="${esc(v[k])}" ${t==='num'?'inputmode="decimal"':''}>`}</div>`).join('')}
        <div class="fld"><label>Effective from</label><input type="date" data-vd="from" value="${v.from}"></div><div class="fld full" data-vderr></div></div>
        <div class="eyebrow mt">History</div>${histTable(coll,s?s.versions:[r],st)}`,
      foot:`<button class="btn dngo" data-vdarch>${ic('archive')}Archive</button><span class="sp"></span><button class="btn" data-a="drawer-close">Cancel</button><button class="btn p" data-vdsave>Save version</button>`,
      mount(dr){
        dr.querySelectorAll('[data-vd]').forEach(i=>i.oninput=i.onchange=()=>{v[i.dataset.vd]=i.value;});
        const err=t=>{dr.querySelector('[data-vderr]').innerHTML=t?`<div class="ferr">${esc(t)}</div>`:'';};
        dr.querySelector('[data-vdarch]').onclick=()=>{UI.closeDrawer();UI.undoable('Archived',()=>{r.status='archived';App.render();});};
        dr.querySelector('[data-vdsave]').onclick=()=>{
          for(const [k,l,t,,opt] of f){if(t==='num'&&!(opt&&v[k].trim()==='')&&(v[k].trim()===''||isNaN(Number(v[k]))||Number(v[k])<0))return err(l+': number ≥ 0');}
          if(coll==='saleMinWeights'&&!(Number(v.minKg)>0))return err('Minimum (kg): above 0');
          if(!/^\d{4}-\d{2}-\d{2}$/.test(v.from))return err('Effective from: date');
          const vals={};f.forEach(([k,,t])=>vals[k]=t==='num'&&v[k].trim()!==''?Number(v[k]):v[k].trim());
          const same=(s?s.versions:[r]).find(x=>(x.from||'')===v.from);
          UI.closeDrawer();
          UI.undoable(same?'Version updated':'New version from '+fmt(v.from),()=>{
            if(same)Object.assign(same,vals);
            else{const base={};DIMS[coll].forEach(d=>base[d]=r[d]||'');S.add(coll,Object.assign(base,coll==='valuationRates'?{stages:r.stages}:{},vals,{from:v.from}));}
            App.render();});
        };}});
  }
  function policyDrawer(){
    const p=policy(),sel=new Set(p.stageIds||[]);const v={toleranceG:String(p.toleranceG),reportKg:String(p.reportKg),from:today()};
    const groups=S.active('species').map(sp=>`<div class="stgrp"><div class="eyebrow">${esc(sp.name)}</div><div class="stchk">${S.active('stages').filter(x=>x.speciesId===sp.id).map(x=>
      `<label><input type="checkbox" data-pst="${x.id}" ${sel.has(x.id)?'checked':''}> ${esc(x.name)}</label>`).join('')}</div></div>`).join('');
    UI.drawer({title:'Sale eligibility',sub:'Sales · Weighing',cls:'idr',wide:1,
      body:`<div class="fgrid"><div class="fld"><label>Weight error margin (g)</label><input data-pd="toleranceG" inputmode="numeric" value="${esc(v.toleranceG)}"></div>
        <div class="fld"><label>Reporting threshold (kg)</label><input data-pd="reportKg" inputmode="decimal" value="${esc(v.reportKg)}"></div>
        <div class="fld"><label>Effective from</label><input type="date" data-pd="from" value="${v.from}"></div><div class="fld"></div>
        <div class="fld full"><label>Eligible stages</label>${groups}</div><div class="fld full" data-pderr></div></div>
        <div class="eyebrow mt">History</div>${histTable('salePolicy',states('salePolicy')[p.id]?states('salePolicy')[p.id].versions:[],states('salePolicy'))}`,
      foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-pdsave>Save version</button>`,
      mount(dr){
        dr.querySelectorAll('[data-pd]').forEach(i=>i.oninput=i.onchange=()=>{v[i.dataset.pd]=i.value;});
        dr.querySelectorAll('[data-pst]').forEach(i=>i.onchange=()=>{i.checked?sel.add(i.dataset.pst):sel.delete(i.dataset.pst);});
        const err=(k,t)=>{dr.querySelectorAll('[data-pd]').forEach(i=>{i.classList.remove('bad');const n=i.parentNode.querySelector('.ferr');if(n)n.remove();});
          if(!t)return;const i=dr.querySelector(`[data-pd="${k}"]`);i.classList.add('bad');i.insertAdjacentHTML('afterend',`<div class="ferr" data-ferr="${k}">${esc(t)}</div>`);i.focus();};
        dr.querySelector('[data-pdsave]').onclick=()=>{
          const tg=Number(v.toleranceG),rk=Number(v.reportKg),tr=String(v.toleranceG).trim();
          if(!/^\d+$/.test(tr))return err('toleranceG',tr===''?'Required':'Whole grams, 0–1000');
          if(tg>1000)return err('toleranceG','Max 1000 g');
          if(String(v.reportKg).trim()===''||isNaN(rk)||rk<=0)return err('reportKg','Above 0 kg');
          if(!/^\d{4}-\d{2}-\d{2}$/.test(v.from))return err('from','Date required');
          err();
          const vals={toleranceG:tg,reportKg:rk,stageIds:[...sel]};const same=S.active('salePolicy').find(x=>(x.from||'')===v.from);
          UI.closeDrawer();
          UI.undoable(same?'Sale eligibility updated':'New version from '+fmt(v.from),()=>{if(same)Object.assign(same,vals);else S.add('salePolicy',Object.assign(vals,{from:v.from}));App.render();});
        };}});
  }
  function historyDrawer(coll){
    const st=states(coll),rows=S.active(coll).slice().sort((a,b)=>(b.from||'').localeCompare(a.from||''));
    const label=coll==='salePolicy'?'Sale eligibility':REG.R[coll].label;
    UI.drawer({title:'History',sub:label,wide:1,cls:'idr',
      body:rows.length?`<div class="sltbl-wrap"><table class="sltbl"><thead><tr><th>Effective from</th><th>Scope</th><th>Value</th><th>Status</th></tr></thead><tbody>${rows.map(r=>`<tr>
        <td>${r.from?esc(fmt(r.from)):'<span class="muted">—</span>'}</td><td>${coll==='salePolicy'?'All':esc(scopeLine(coll,r))}</td>
        <td class="num">${coll==='salePrices'?inr(r.price)+(r.basis==='per_kg'?'/kg':'/animal'):coll==='saleMinWeights'?esc(r.minKg)+' kg':coll==='valuationRates'?inr(r.rate)+'/kg':esc(r.toleranceG)+' g · '+esc(r.reportKg)+' kg'}</td>
        <td>${stateTag(st[r.id])}</td></tr>`).join('')}</tbody></table></div>`:'<div class="empty">No versions</div>'});
  }
  Object.assign(window.A=window.A||{},{
    'sl-policy':()=>policyDrawer(),
    'sl-hist':el=>historyDrawer(el.dataset.reg),
    'sl-grid':el=>{const k=el.dataset.reg,ids=S.active(k).map(r=>r.id),o=List.opts[k]||{};ids.length?Sheet.openEntry(k,ids):Sheet.openEntry(k,null,[o.defaults?o.defaults():{}]);}
  });
  /* ---------- connected outputs: Weighing operator verdict, Weighing and Sales module pages ---------- */
  function animalOf(ans){const vals=Object.values(ans||{}).map(x=>REG.idNorm(x));return S.active('animals').find(a=>vals.includes(REG.idNorm(a.rfid)))||null;}
  const VERD={ready:['Sale-ready','ok'],margin:['Borderline','warn'],below:['Below sale weight','mut']},SHORT={ready:'Sale-ready',margin:'Borderline',below:'Below'};
  X.opVerdict=function(n,sop,r){
    if(!sop||sop.dept!=='Weighing'||n.unit!=='kg'||!/^weight$/i.test(String(n.label||'').trim()))return '';
    const a=animalOf(r.ans)||{speciesId:'sp_goat'},raw=r.ans[n.id],w=raw===''||raw==null?'':Number(raw);
    const ws=weighState(a,w),p=policy(),pill=(t,tone)=>`<span class="op-pill ${tone}">${esc(t)}</span>`;
    if(!ws.mw)return '';
    const v=ws.state&&VERD[ws.state];
    return `<div class="op-row op-lims" data-salev="${ws.state||''}">${v?pill(v[0],v[1]):''}${pill('Sale weight '+ws.mw.minKg+' kg · margin '+(+p.toleranceG||0)+' g','mut')}</div>`;
  };
  function readiness(){
    const p=policy(),rows={};let c={ready:0,margin:0,below:0,none:0};
    S.active('animals').forEach(a=>{const ws=weighState(a,a.weight);const k=ws.state||'none';c[k]++;
      const key=REG.labelById('species',a.speciesId)+'|'+REG.labelById('stages',a.stageId);const g=rows[key]=rows[key]||{ready:0,margin:0,below:0,none:0,n:0};g[k]++;g.n++;});
    return {c,p,rows};
  }
  const kp=(l,v,sub)=>`<div class="kpi"><div class="lab">${esc(l)}</div><div class="val">${v}</div>${sub?`<div class="dl">${sub}</div>`:''}</div>`;
  const cfgLink=(l,h)=>`<a class="btn sm" href="${h}">${ic('settings')}${esc(l)}</a>`;
  X.weighingCard=function(){
    const {c,p,rows}=readiness(),mins=current('saleMinWeights').filter(r=>!r.breedId&&!r.sexId);
    return `<section class="card mt" data-salecard="weighing"><div class="hd lhd"><div class="ttl"><div class="eyebrow">Sample herd</div><h3>Sale readiness</h3></div><span class="sp"></span>${cfgLink('Sale eligibility','#/configuration/items/saleEligibility')}</div>
      <div class="grid g4 slkpi" style="padding-top:14px">${kp('Sale-ready',`<span data-rc="ready">${c.ready}</span>`,mins.map(m=>esc(REG.labelById('species',m.speciesId)+' ≥ '+m.minKg+' kg')).join(' · '))}${kp('Borderline',`<span data-rc="margin">${c.margin}</span>`,'Margin '+esc(p.toleranceG)+' g')}${kp('Below sale weight',`<span data-rc="below">${c.below}</span>`)}${kp('Reporting threshold',esc(p.reportKg)+'<small> kg</small>')}</div>
      <div class="twrap screen ltable"><table class="ltbl"><thead><tr><th>Species · stage</th><th class="num">Animals</th><th class="num">Sale-ready</th><th class="num">Borderline</th><th class="num">Below</th></tr></thead><tbody>${
        Object.entries(rows).sort().map(([k,g])=>`<tr><td data-label="Species · stage"><b>${esc(k.replace('|',' · '))}</b></td><td class="num" data-label="Animals">${g.n}</td><td class="num" data-label="Sale-ready">${g.ready}</td><td class="num" data-label="Borderline">${g.margin}</td><td class="num" data-label="Below">${g.below}</td></tr>`).join('')}</tbody></table></div></section>`;
  };
  function valuationOf(a){
    const st=S.get('stages',a.stageId),code=st?st.code:'',rates=current('valuationRates'),by=n=>rates.find(r=>r.name===n);
    const g=/^K[0-3]$/.test(code)?by(code):/^FAT/.test(code)?by('Fattening animals'):a.sexId==='sx_female'?by('Adult females'):a.sexId==='sx_male'?by('Adult males / bucks'):null;
    if(!g)return null;const w=g.assumedKg===''||g.assumedKg==null?Number(a.weight):Number(g.assumedKg);
    return isNaN(w)?null:{g,value:g.rate*w};
  }
  X.valueCard=function(){
    const an=S.active('animals');let tp=0,tv=0,np=0;
    const rows=an.map(a=>{const m=match('salePrices',a).rule,ws=weighState(a,a.weight),val=valuationOf(a);
      const price=m?(m.basis==='per_animal'?m.price:m.price*Number(a.weight)):null;if(price!=null&&!isNaN(price)){tp+=price;np++;}if(val)tv+=val.value;
      return [`<b class="mono">${esc(a.rfid.slice(-6))}</b>`,esc(REG.labelById('breeds',a.breedId)),esc(REG.labelById('sexes',a.sexId)),esc(REG.labelById('stages',a.stageId)),esc(a.weight),
        ws.state?UI.tag(SHORT[ws.state],ws.state==='ready'?'ok':ws.state==='margin'?'warn':'mut'):'',
        price!=null&&!isNaN(price)?`<span data-est>${inr(Math.round(price))}</span>`:'<span class="muted">No rule</span>',val?inr(Math.round(val.value)):'<span class="muted">—</span>'];});
    const H=[['Animal'],['Breed'],['Sex'],['Stage'],['Weight (kg)',1],['Sale weight'],['Sale price',1],['Farm value',1]];
    return `<section class="card mt" data-salecard="value"><div class="hd lhd"><div class="ttl"><div class="eyebrow">Sample herd</div><h3>Value per animal <span class="cnt">${an.length}</span></h3></div><span class="sp"></span>${cfgLink('Sale price rules','#/configuration/items/salePrices')}${cfgLink('Valuation rates','#/configuration/items/valuationRates')}</div>
      <div class="grid g3 slkpi" style="padding-top:14px">${kp('Estimated sale value',`<span data-tot="price">${np?inr(Math.round(tp)):'—'}</span>`,np+' of '+an.length+' priced')}${kp('Farm value',`<span data-tot="value">${inr(Math.round(tv))}</span>`)}${kp('Price rules in force',current('salePrices').length)}</div>
      <div class="twrap screen ltable"><table class="ltbl"><thead><tr>${H.map(h=>`<th class="${h[1]?'num':''}">${h[0]}</th>`).join('')}</tr></thead><tbody>${rows.map(r=>`<tr>${r.map((c,i)=>`<td class="${H[i][1]?'num':''}" data-label="${H[i][0]}">${c}</td>`).join('')}</tr>`).join('')}</tbody></table></div></section>`;
  };
  X.match=match;X.policy=policy;X.states=states;
  window.Sales=X;
})();
