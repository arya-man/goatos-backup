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
  const TIE={stageId:4,breedId:2,sexId:1};
  const rank=(coll,r)=>spec(coll,r)*10+DIMS[coll].slice(1).reduce((n,d)=>n+(r[d]?TIE[d]||0:0),0);
  const pad2=n=>String(n).padStart(2,'0');
  const toDMY=iso=>iso&&/^\d{4}-\d{2}-\d{2}$/.test(iso)?iso.slice(8,10)+'/'+iso.slice(5,7)+'/'+iso.slice(0,4):'';
  const fromDMY=t=>{const m=String(t||'').trim().match(/^(\d{1,2})\/(\d{1,2})\/(\d{4})$/);if(!m)return '';const d=new Date(+m[3],+m[2]-1,+m[1]);
    return d.getDate()===+m[1]&&d.getMonth()===+m[2]-1?m[3]+'-'+pad2(m[2])+'-'+pad2(m[1]):'';};

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

  /* most specific current rule wins; equal specificity: stage beats breed beats sex */
  function match(coll,a){
    const hits=current(coll).filter(r=>r.speciesId===a.speciesId&&DIMS[coll].slice(1).every(d=>!r[d]||r[d]===a[d]))
      .sort((x,y)=>rank(coll,y)-rank(coll,x));
    const top=hits[0]||null;
    return {rule:top,conflict:top?hits.filter(h=>h!==top&&spec(coll,h)===spec(coll,top)):[]};
  }
  /* pairs of current rules with equal specificity that can both match one animal */
  function conflictPairs(coll){
    const cur=current(coll),out=[];
    cur.forEach((a,i)=>cur.slice(i+1).forEach(b=>{if(a.speciesId!==b.speciesId||spec(coll,a)!==spec(coll,b))return;
      if(DIMS[coll].slice(1).every(d=>!a[d]||!b[d]||a[d]===b[d])){const w=rank(coll,a)>=rank(coll,b)?a:b;out.push({a,b,win:w,lose:w===a?b:a});}}));
    return out;
  }
  function conflicts(coll){const out={};conflictPairs(coll).forEach(p=>{(out[p.a.id]=out[p.a.id]||[]).push(p.b);(out[p.b.id]=out[p.b.id]||[]).push(p.a);});return out;}
  function appliesTo(coll){
    const n={};S.active('animals').forEach(a=>{const m=match(coll,a);if(m.rule)n[m.rule.id]=(n[m.rule.id]||0)+1;});return n;
  }
  function weighState(a,w){
    const mw=match('saleMinWeights',a).rule,p=policy(),tol=(+p.toleranceG||0)/1000;
    if(!mw||w===''||w==null||isNaN(w))return {mw,state:'',tol};
    return {mw,tol,state:+w>=mw.minKg?'ready':+w>=mw.minKg-tol?'margin':'below'};
  }

  /* one helper for Sale eligibility, Weighing, Farm value and the preview:
     stage not in eligible stages → ineligible; weight ≥ min − margin → ready (margin flag when under min) */
  function saleState(a,w){
    const p=policy();
    if(!(p.stageIds||[]).includes(a.stageId))return {state:'ineligible',margin:false,mw:null,tol:(+p.toleranceG||0)/1000};
    const ws=weighState(a,w);
    return {mw:ws.mw,tol:ws.tol,state:ws.state==='margin'?'ready':ws.state||'none',margin:ws.state==='margin'};
  }
  X.saleState=saleState;
  X.readiness=function(){
    const c={eligible:0,ready:0,margin:0,below:0,none:0,ineligible:0},rows={},by={};
    S.active('animals').forEach(a=>{const r=saleState(a,a.weight);by[a.id]=r;c[r.state]++;if(r.state==='ineligible')return;c.eligible++;if(r.margin)c.margin++;
      const key=REG.labelById('species',a.speciesId)+'|'+REG.labelById('stages',a.stageId);const g=rows[key]=rows[key]||{ready:0,margin:0,below:0,none:0,n:0};g[r.state]++;if(r.margin)g.margin++;g.n++;});
    return {c,rows,by,p:policy()};
  };
  const marginNote=(n,p)=>n?n+' within '+(+p.toleranceG||0)+' g margin':'Margin '+(+p.toleranceG||0)+' g';
  const usedBy=list=>`<div class="usedby"><span class="muted small">Used by</span>${list.map(([l,h])=>`<a class="chip" href="${h}">${esc(l)}</a>`).join('')}</div>`;
  const U={w:['Weighing','#/weighing/analytics'],o:['Weighing SOP','#/weighing/sops'],f:['Farm value','#/sales/farm-value']};
  const stateTag=(s,extra)=>(s.dup?UI.tag('Duplicate','dng')+' ':'')+(extra&&extra.length?UI.tag('Conflict','dng')+' ':'')+(s.state==='current'?UI.tag('Current','ok'):s.state==='scheduled'?UI.tag('Scheduled','info'):UI.tag('Superseded','mut'));
  const sortScope=coll=>(a,b)=>DIMS[coll].map(d=>{const c=d==='name'?null:d.replace(/Id$/,'')==='sex'?'sexes':d.replace(/Id$/,'')==='species'?'species':d.replace(/Id$/,'')+'s';
      return (c?(a[d]?REG.labelById(c,a[d]):''):a[d]||'').localeCompare(c?(b[d]?REG.labelById(c,b[d]):''):b[d]||'');}).find(x=>x)||(a.from||'').localeCompare(b.from||'');
  const ruleName=(coll,r)=>[REG.labelById('species',r.speciesId)].concat(DIMS[coll].slice(1).filter(d=>r[d]).map(d=>REG.labelById(d==='sexId'?'sexes':d==='breedId'?'breeds':'stages',r[d]))).join(' · ');
  const ruleVal=(coll,r)=>coll==='salePrices'?inr(r.price)+(r.basis==='per_kg'?' /kg':' /animal'):r.minKg+' kg';
  function clashBanner(coll,st){
    const pairs=conflictPairs(coll),dups=S.active(coll).filter(r=>st[r.id]&&st[r.id].dup&&st[r.id].state!=='superseded');
    if(!pairs.length&&!dups.length)return '';
    const ed=r=>`<a href="javascript:void 0" data-a="sl-edit" data-reg="${coll}" data-id="${r.id}">Edit</a>`;
    const DN={stageId:'stage',breedId:'breed',sexId:'sex'};
    const why=(w,l)=>{const d=DIMS[coll].slice(1).filter(x=>w[x]&&!l[x]).sort((a,b)=>TIE[b]-TIE[a])[0],e=DIMS[coll].slice(1).filter(x=>l[x]&&!w[x])[0];return d&&e?DN[d]+' beats '+DN[e]:'more specific';};
    return `<div class="slwarn slclash"><div><b>${pairs.length+dups.length} clash${pairs.length+dups.length===1?'':'es'}</b></div><ul>${
      pairs.map(p=>`<li><span>${esc(ruleName(coll,p.a))} <span class="muted">${esc(ruleVal(coll,p.a))}</span> ${ed(p.a)}</span><span class="muted">vs</span><span>${esc(ruleName(coll,p.b))} <span class="muted">${esc(ruleVal(coll,p.b))}</span> ${ed(p.b)}</span><span class="slwin">${esc(ruleName(coll,p.win))} wins (${why(p.win,p.lose)})</span></li>`).join('')
      +dups.map(r=>`<li><span>${esc(ruleName(coll,r))} <span class="muted">${esc(ruleVal(coll,r))}</span> ${ed(r)}</span><span class="slwin">Two versions share one start date · archive one</span></li>`).join('')}</ul></div>`;
  }
  function list(k,opts){List.opts[k]=opts;return List.html(k,opts);}
  const gridBtn=k=>`<button class="btn sm" data-a="sl-grid" data-reg="${k}">${ic('sheet')}Edit in grid</button>`;
  const histBtn=k=>`<button class="btn sm" data-a="sl-hist" data-reg="${k}">${ic('rotate')}History</button>`;

  /* ---------- panels ---------- */
  X.prices=function(){
    const st=states('salePrices'),cf=conflicts('salePrices'),n=appliesTo('salePrices');
    return usedBy([U.f])+clashBanner('salePrices',st)
      +list('salePrices',{eyebrow:'Sales',title:'Sale price rules',filters:['species','basis'],addLabel:'Add rule',sort:sortScope('salePrices'),noStatus:1,
        onNew:()=>ruleDrawer('salePrices',null),onOpen:id=>ruleDrawer('salePrices',id),extraBtns:histBtn('salePrices')+gridBtn('salePrices'),
        columns:[{label:'Species',w:84,html:r=>`<b>${esc(REG.labelById('species',r.speciesId))}</b>`},{label:'Breed',w:110,html:r=>any('breeds',r.breedId)},
          {label:'Sex',w:70,html:r=>any('sexes',r.sexId)},{label:'Stage',w:130,html:r=>any('stages',r.stageId)},
{label:'Price',w:120,html:r=>`<span class="num">${inr(r.price)}</span><span class="muted small">${r.basis==='per_kg'?' /kg':' /animal'}</span>`},
          {label:'Animals',w:76,html:r=>st[r.id]&&st[r.id].state==='current'?`<span class="num">${n[r.id]||0}</span>`:'<span class="muted">—</span>'},
          {label:'Version',w:170,wrap:1,html:r=>(st[r.id]?stateTag(st[r.id],cf[r.id]):UI.tag('Archived','mut'))+(r.from?`<div class="muted small">From ${esc(fmt(r.from))}</div>`:'')}]})
      +preview();
  };

  X.eligibility=function(){
    const p=policy(),st=states('saleMinWeights'),n=appliesTo('saleMinWeights'),cf=conflicts('saleMinWeights');
    const stages=(p.stageIds||[]).map(id=>S.get('stages',id)).filter(Boolean);
    const {c}=X.readiness();
    return usedBy([U.w,U.o,U.f])+clashBanner('saleMinWeights',st)
      +`<section class="card"><div class="hd lhd"><div class="ttl"><div class="eyebrow">Sales</div><h3>Sale eligibility</h3></div><span class="sp"></span>${histBtn('salePolicy')}<button class="btn sm p" data-a="sl-policy">${ic('edit-3')}Edit</button></div>
        <div class="slpol">
          <div class="kv"><div class="lab">Weight error margin</div><div class="val"><span class="num">${esc(p.toleranceG)}</span> g</div></div>
          <div class="kv"><div class="lab">Reporting threshold</div><div class="val"><span class="num">${esc(p.reportKg)}</span> kg</div></div>
          <div class="kv"><div class="lab">Effective from</div><div class="val">${p.from?esc(fmt(p.from)):'<span class="muted">—</span>'}</div></div>
          <div class="kv wide"><div class="lab">Eligible stages</div><div class="chips">${stages.length?stages.map(s=>UI.tag(s.name+' · '+REG.labelById('species',s.speciesId),'teal')).join(''):'<span class="muted">None</span>'}</div></div>
        </div>
        <div class="grid g3 slkpi">${kp('In eligible stages',`<span data-rc="eligible">${c.eligible}</span>`,c.ineligible+' in other stages')+kp('Sale-ready',`<span data-rc="ready">${c.ready}</span>`,marginNote(c.margin,p))+kp('Below sale weight',`<span data-rc="below">${c.below}</span>`,c.none?c.none+' not weighed or no minimum':'')}</div>
      </section>`
      +`<div class="mt">${list('saleMinWeights',{eyebrow:'Sale eligibility',title:'Minimum sale weight',filters:['species'],addLabel:'Add override',sort:sortScope('saleMinWeights'),
        noStatus:1,onNew:()=>ruleDrawer('saleMinWeights',null),onOpen:id=>ruleDrawer('saleMinWeights',id),extraBtns:histBtn('saleMinWeights')+gridBtn('saleMinWeights'),
        columns:[{label:'Species',html:r=>`<b>${esc(REG.labelById('species',r.speciesId))}</b>`},{label:'Breed',html:r=>any('breeds',r.breedId)},{label:'Sex',w:80,html:r=>any('sexes',r.sexId)},
          {label:'Minimum',w:110,html:r=>`<span class="num">${esc(r.minKg)}</span> kg`},{label:'Effective from',w:120,html:r=>r.from?esc(fmt(r.from)):'<span class="muted">—</span>'},
          {label:'Applies to',w:100,html:r=>st[r.id]&&st[r.id].state==='current'?`<span class="num">${n[r.id]||0}</span> <span class="muted small">animals</span>`:'<span class="muted">—</span>'},
          {label:'Version',w:200,wrap:1,html:r=>st[r.id]?stateTag(st[r.id],cf[r.id]):UI.tag('Archived','mut')}]})}</div>`
      +preview();
  };

  X.valuation=function(){
    const st=states('valuationRates');
    return usedBy([U.f])
      +list('valuationRates',{eyebrow:'Sales',title:'Valuation rates',addLabel:'Add group',sort:(a,b)=>0,noStatus:1,defaults:()=>({from:today()}),
        onOpen:id=>versionDrawer('valuationRates',id),extraBtns:histBtn('valuationRates')+gridBtn('valuationRates'),
        columns:[{label:'Group',html:r=>`<b>${esc(r.name)}</b>`},{label:'Stages',low:1,html:r=>esc(r.stages||'')},{label:'Rate',w:120,html:r=>`<span class="num">${inr(r.rate)}</span><span class="muted small"> /kg</span>`},
          {label:'Assumed weight',w:150,html:r=>r.assumedKg===''||r.assumedKg==null?'<span class="muted">Measured average</span>':`<span class="num">${esc(r.assumedKg)}</span> kg`},
          {label:'Value per animal',w:140,low:1,html:r=>r.assumedKg===''||r.assumedKg==null?'<span class="muted">—</span>':`<span class="num">${inr(r.rate*r.assumedKg)}</span>`},
          {label:'Effective from',w:120,html:r=>r.from?esc(fmt(r.from)):'<span class="muted">—</span>'},{label:'Version',w:200,wrap:1,html:r=>st[r.id]?stateTag(st[r.id]):UI.tag('Archived','mut')}]});
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
    const pm=match('salePrices',a),ws=weighState(a,w),ss=saleState(a,w),stageOk=ss.state!=='ineligible';
    const row=(l,v)=>`<div class="pvr"><span class="muted">${l}</span><span>${v}</span></div>`;
    const est=pm.rule&&(pm.rule.basis==='per_animal'?inr(pm.rule.price):w===''||isNaN(w)?'<span class="muted">Weight needed</span>':inr(pm.rule.price*w));
    return row('Price rule',pm.rule?`<b>${esc(REG.labelById('species',pm.rule.speciesId)+' · '+scopeText('salePrices',pm.rule))}</b> · ${inr(pm.rule.price)}${pm.rule.basis==='per_kg'?'/kg':'/animal'}${pm.conflict.length?' '+UI.tag('Conflict','dng'):''}`:UI.tag('No rule','warn'))
      +row('Minimum weight',ws.mw?`<b>${esc(ws.mw.minKg)} kg</b> · ${esc(scopeText('saleMinWeights',ws.mw))}`:UI.tag('Not set','warn'))
      +row('Eligible stage',a.stageId?(stageOk?UI.tag('Yes','ok'):UI.tag('No','mut')):'<span class="muted">—</span>')
      +row('Within error margin',ws.state==='margin'?UI.tag('Yes · '+Math.round((ws.mw.minKg-w)*1000)+' g under','warn'):ws.state?`<span class="muted">No</span>`:'<span class="muted">—</span>')
      +row('Sale-ready',!ws.state||!a.stageId?'<span class="muted">—</span>':ss.state==='ready'?UI.tag(ss.margin?'Yes · within '+(+p.toleranceG||0)+' g margin':'Yes','ok'):UI.tag('No','dng'))
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
  const FIELDS={salePrices:[['basis','Unit','enum',['per_kg','per_animal']],['price','Price (₹)','num']],saleMinWeights:[['minKg','Minimum (kg)','num']],
    valuationRates:[['rate','Rate (₹ per kg)','num'],['assumedKg','Assumed weight (kg)','num',null,1],['stages','Stages','text']]};
  function scopeLine(coll,r){return coll==='valuationRates'?r.name:REG.labelById('species',r.speciesId)+' · '+scopeText(coll,r);}
  function histTable(coll,rows,st){
    const f=FIELDS[coll]||[];
    return `<div class="sltbl-wrap"><table class="sltbl"><thead><tr><th>Effective from</th>${coll==='salePolicy'?'<th>Margin</th><th>Reporting</th><th>Stages</th>':f.map(x=>`<th>${esc(x[1])}</th>`).join('')}<th>Status</th></tr></thead><tbody>${
      rows.slice().reverse().map(r=>`<tr><td>${r.from?esc(fmt(r.from)):'<span class="muted">—</span>'}</td>${coll==='salePolicy'?`<td class="num">${esc(r.toleranceG)} g</td><td class="num">${esc(r.reportKg)} kg</td><td>${(r.stageIds||[]).length}</td>`
        :f.map(x=>`<td>${x[2]==='enum'?esc(r[x[0]]==='per_kg'?'/kg live':'/animal'):esc(r[x[0]]===''||r[x[0]]==null?'—':r[x[0]])}</td>`).join('')}<td>${st[r.id]?stateTag(st[r.id]):''}</td></tr>`).join('')}</tbody></table></div>`;
  }
  const DH=(icon,crumb,title)=>`<div class="ihead"><span class="ikic">${ic(icon,'',18)}</span><div class="ihead-t"><div class="icrumb">${esc(crumb)}</div><h2>${esc(title)}</h2></div></div>`;
  const SEC=(title,inner,extra)=>`<section class="isec"><div class="isec-h"><h4>${esc(title)}</h4>${extra||''}</div><div class="isec-b">${inner}</div></section>`;
  function versionDrawer(coll,id){
    const r=S.get(coll,id);if(!r)return;const st=states(coll),s=st[r.id],f=FIELDS[coll];
    const v={from:toDMY(today())};f.forEach(x=>v[x[0]]=r[x[0]]==null?'':String(r[x[0]]));
    UI.drawer({head:DH('banknote',REG.R[coll].label,scopeLine(coll,r)),cls:'idr',wide:1,
      body:SEC(REG.R[coll].one,`<div class="fgrid">${f.map(([k,l,t,o])=>`<div class="fld"><label>${esc(l)}</label>${t==='enum'?`<select data-vd="${k}">${o.map(x=>`<option value="${x}" ${v[k]===x?'selected':''}>${esc(REG.enumLabel(x))}</option>`).join('')}</select>`:`<input data-vd="${k}" value="${esc(v[k])}" ${t==='num'?'inputmode="decimal"':''}>`}</div>`).join('')}
        <div class="fld"><label>Effective from</label><input data-vd="from" inputmode="numeric" placeholder="DD/MM/YYYY" value="${v.from}"></div></div>`)+SEC('History',histTable(coll,s?s.versions:[r],st)),
      foot:`<button class="btn dngo" data-vdarch>${ic('archive')}Archive</button><span class="sp"></span><button class="btn" data-a="drawer-close">Cancel</button><button class="btn p" data-vdsave>Save version</button>`,
      mount(dr){
        dr.querySelectorAll('[data-vd]').forEach(i=>i.oninput=i.onchange=()=>{v[i.dataset.vd]=i.value;});
        const err=(k,t)=>fieldErr(dr,k,t);
        dr.querySelector('[data-vdarch]').onclick=()=>{UI.closeDrawer();UI.undoable('Archived',()=>{r.status='archived';App.render();});};
        dr.querySelector('[data-vdsave]').onclick=()=>{
          for(const [k,l,t,,opt] of f){if(t==='num'&&!(opt&&v[k].trim()==='')&&(v[k].trim()===''||isNaN(Number(v[k]))||Number(v[k])<0))return err(k,k==='assumedKg'?'Enter a weight of 0 or more, or leave blank':'Enter a rate of 0 or more');}
          const iso=fromDMY(v.from);if(!iso)return err('from','Enter a date as DD/MM/YYYY');err();v.from=iso;
          const vals={};f.forEach(([k,,t])=>vals[k]=t==='num'&&v[k].trim()!==''?Number(v[k]):v[k].trim());
          const same=(s?s.versions:[r]).find(x=>(x.from||'')===v.from);
          UI.closeDrawer();
          UI.undoable(same?'Version updated':'New version from '+fmt(v.from),()=>{
            if(same)Object.assign(same,vals);
            else{const base={};DIMS[coll].forEach(d=>base[d]=r[d]||'');S.add(coll,Object.assign(base,coll==='valuationRates'?{stages:r.stages}:{},vals,{from:v.from}));}
            App.render();});
        };}});
  }
  const fieldErr=(dr,k,t)=>{dr.querySelectorAll('.bad').forEach(i=>i.classList.remove('bad'));dr.querySelectorAll('.ferr[data-ferr]').forEach(n=>n.remove());
    if(!t)return;const i=dr.querySelector(`[data-rf="${k}"]`)||dr.querySelector(`[data-pd="${k}"]`)||dr.querySelector(`[data-vd="${k}"]`);if(!i)return;const box=i.closest('.inpfx')||i;box.classList.add('bad');
    (i.closest('.fld')||box).insertAdjacentHTML('beforeend',`<div class="ferr" data-ferr="${k}">${esc(t)}</div>`);i.focus&&i.focus();};
  function ruleDrawer(coll,id){
    const r=id?S.get(coll,id):null;if(id&&!r)return;
    const isP=coll==='salePrices',st=states(coll),s=r&&st[r.id];
    const v=r?{speciesId:r.speciesId,breedId:r.breedId||'',sexId:r.sexId||'',stageId:r.stageId||'',price:r.price==null?'':String(r.price),basis:r.basis||'per_kg',minKg:r.minKg==null?'':String(r.minKg),from:toDMY(today()),newBreed:''}
      :{speciesId:(S.active('species')[0]||{}).id||'',breedId:'',sexId:'',stageId:'',price:'',basis:'per_kg',minKg:'',from:toDMY(today()),newBreed:''};
    const segs=(k,opts)=>`<div class="seg" data-rfseg="${k}">${opts.map(([val,l])=>`<button type="button" class="${v[k]===val?'on':''}" data-v="${val}">${esc(l)}</button>`).join('')}</div>`;
    const body=()=>SEC(isP?'Price rule':'Minimum sale weight',`<div class="fgrid">
        <div class="fld"><label>Species</label><select data-rf="speciesId">${S.active('species').map(x=>`<option value="${x.id}" ${x.id===v.speciesId?'selected':''}>${esc(x.name)}</option>`).join('')}</select></div>
        <div class="fld"><label>Breed</label><select data-rf="breedId"><option value="">Any breed</option>${S.active('breeds').filter(x=>x.speciesId===v.speciesId).map(x=>`<option value="${x.id}" ${x.id===v.breedId?'selected':''}>${esc(x.name)}</option>`).join('')}<option value="__new" ${v.breedId==='__new'?'selected':''}>+ New breed…</option></select>
          ${v.breedId==='__new'?`<input data-rf="newBreed" class="mt6" placeholder="Breed name" value="${esc(v.newBreed)}">`:''}</div>
        <div class="fld"><label>Sex</label>${segs('sexId',[['','Any'],['sx_female','Female'],['sx_male','Male']])}</div>
        ${isP?`<div class="fld"><label>Stage</label><select data-rf="stageId"><option value="">Any stage</option>${S.active('stages').filter(x=>x.speciesId===v.speciesId).map(x=>`<option value="${x.id}" ${x.id===v.stageId?'selected':''}>${esc(x.name)}</option>`).join('')}</select></div>
        <div class="fld"><label>Price</label><div class="inpfx"><span>₹</span><input data-rf="price" inputmode="decimal" value="${esc(v.price)}" placeholder="0"></div></div>
        <div class="fld"><label>Unit</label>${segs('basis',[['per_kg','/kg live'],['per_animal','/animal']])}</div>`
        :`<div class="fld"><label>Minimum sale weight</label><div class="inpfx sfx"><input data-rf="minKg" inputmode="decimal" value="${esc(v.minKg)}" placeholder="35"><span>kg</span></div></div>`}
        <div class="fld"><label>Effective from</label><input data-rf="from" inputmode="numeric" placeholder="DD/MM/YYYY" value="${esc(v.from)}"></div></div>`)+(r?SEC('History',histTable(coll,s?s.versions:[r],st)):'');
    UI.drawer({head:DH('banknote',isP?'Sale price rules':'Sale eligibility',r?ruleName(coll,r):(isP?'Add price rule':'Add minimum sale weight')),cls:'idr',wide:1,body:body(),
      foot:`${r?`<button class="btn dngo" data-rfarch>${ic('archive')}Archive</button>`:''}<span class="sp"></span><button class="btn" data-a="drawer-close">Cancel</button><button class="btn p" data-rfsave>${r?'Save version':'Add'}</button>`,
      mount(dr){
        const wire=()=>{
          dr.querySelectorAll('[data-rf]').forEach(i=>i.oninput=i.onchange=e=>{v[i.dataset.rf]=i.value;
            if(e.type==='change'&&(i.dataset.rf==='speciesId'||i.dataset.rf==='breedId')){if(i.dataset.rf==='speciesId'){v.breedId='';v.stageId='';}redraw(i.dataset.rf==='breedId'&&v.breedId==='__new'?'newBreed':null);}});
          dr.querySelectorAll('[data-rfseg]').forEach(g=>g.querySelectorAll('button').forEach(b=>b.onclick=()=>{v[g.dataset.rfseg]=b.dataset.v;g.querySelectorAll('button').forEach(x=>x.classList.toggle('on',x===b));}));
        };
        const redraw=focus=>{dr.querySelector('.dc').innerHTML=body();wire();if(focus){const f=dr.querySelector(`[data-rf="${focus}"]`);f&&f.focus();}};
        wire();
        if(r)dr.querySelector('[data-rfarch]').onclick=()=>{UI.closeDrawer();UI.undoable('Archived',()=>{r.status='archived';App.render();});};
        dr.querySelector('[data-rfsave]').onclick=()=>{
          if(!v.speciesId)return fieldErr(dr,'speciesId','Choose a species');
          if(v.breedId==='__new'){const nm=v.newBreed.trim();if(!nm)return fieldErr(dr,'newBreed','Enter a breed name');
            if(S.active('breeds').some(b=>b.speciesId===v.speciesId&&REG.norm(b.name)===REG.norm(nm)))return fieldErr(dr,'newBreed','That breed already exists');}
          if(isP){const t=v.price.trim();if(t===''||isNaN(Number(t))||Number(t)<0)return fieldErr(dr,'price','Enter a price of 0 or more');}
          else{const t=v.minKg.trim();if(t===''||isNaN(Number(t))||Number(t)<=0)return fieldErr(dr,'minKg','Enter a weight above 0 kg');}
          const iso=fromDMY(v.from);if(!iso)return fieldErr(dr,'from','Enter a date as DD/MM/YYYY');
          fieldErr(dr);
          UI.closeDrawer();
          UI.undoable(r?(isP?'Price rule saved':'Minimum sale weight saved'):(isP?'Price rule added':'Minimum sale weight added'),()=>{
            let breedId=v.breedId;if(breedId==='__new')breedId=S.add('breeds',{name:v.newBreed.trim(),speciesId:v.speciesId,aliases:[]}).id;
            const scope={speciesId:v.speciesId,breedId,sexId:v.sexId};if(isP)scope.stageId=v.stageId;
            const vals=isP?{price:Number(v.price),basis:v.basis}:{minKg:Number(v.minKg)};
            const same=r&&DIMS[coll].every(d=>(r[d]||'')===(scope[d]||''));
            const twin=S.active(coll).find(x=>DIMS[coll].every(d=>(x[d]||'')===(scope[d]||''))&&(x.from||'')===iso)||(same&&(r.from||'')===iso?r:null);
            if(twin)Object.assign(twin,vals);
            else if(r&&!same&&(r.from||'')===iso)Object.assign(r,scope,vals);
            else S.add(coll,Object.assign(scope,vals,{from:iso}));
            App.render();});
        };}});
  }
  function policyDrawer(){
    const p=policy(),sel=new Set(p.stageIds||[]);const v={toleranceG:String(p.toleranceG),reportKg:String(p.reportKg),from:toDMY(today())};
    const grp=x=>/^K\d$/.test(x.code||'')?'Kids':/^FAT/.test(x.code||'')?'Fattening':'Adults';
    const groups=S.active('species').map(sp=>{const ss=S.active('stages').filter(x=>x.speciesId===sp.id);
      return SEC(sp.name,`<div class="fgrid">${['Fattening','Adults','Kids'].map(g=>{const gs=ss.filter(x=>grp(x)===g);return gs.length?`<div class="fld full"><label>${g}</label><div class="chips">${gs.map(x=>
        `<button type="button" class="chip ${sel.has(x.id)?'on':''}" data-pst="${x.id}" data-sp="${sp.id}" aria-pressed="${sel.has(x.id)}">${esc(x.name)}</button>`).join('')}</div></div>`:'';}).join('')}</div>`,`<span class="cnt" data-pstn="${sp.id}">${ss.filter(x=>sel.has(x.id)).length}</span>`);}).join('');
    UI.drawer({head:DH('banknote','Sales · Weighing','Sale eligibility'),cls:'idr',wide:1,
      body:SEC('Weights',`<div class="fgrid"><div class="fld"><label>Weight error margin (g)</label><input data-pd="toleranceG" inputmode="numeric" value="${esc(v.toleranceG)}"></div>
        <div class="fld"><label>Reporting threshold (kg)</label><input data-pd="reportKg" inputmode="decimal" value="${esc(v.reportKg)}"></div>
        <div class="fld"><label>Effective from</label><input data-pd="from" inputmode="numeric" placeholder="DD/MM/YYYY" value="${v.from}"></div></div>`)+groups+SEC('History',histTable('salePolicy',states('salePolicy')[p.id]?states('salePolicy')[p.id].versions:[],states('salePolicy'))),
      foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-pdsave>Save version</button>`,
      mount(dr){
        dr.querySelectorAll('[data-pd]').forEach(i=>i.oninput=i.onchange=()=>{v[i.dataset.pd]=i.value;});
        dr.querySelectorAll('[data-pst]').forEach(b=>b.onclick=()=>{const on=!sel.has(b.dataset.pst);on?sel.add(b.dataset.pst):sel.delete(b.dataset.pst);b.classList.toggle('on',on);b.setAttribute('aria-pressed',on);
          const n=dr.querySelector(`[data-pstn="${b.dataset.sp}"]`);if(n)n.textContent=[...dr.querySelectorAll(`[data-pst][data-sp="${b.dataset.sp}"]`)].filter(x=>sel.has(x.dataset.pst)).length;});
        const err=(k,t)=>fieldErr(dr,k,t);
        dr.querySelector('[data-pdsave]').onclick=()=>{
          const tg=Number(v.toleranceG),rk=Number(v.reportKg),tr=String(v.toleranceG).trim();
          if(!/^\d+$/.test(tr))return err('toleranceG',tr===''?'Enter a margin in grams':'Enter whole grams from 0 to 1000');
          if(tg>1000)return err('toleranceG','Enter 1000 g or less');
          if(String(v.reportKg).trim()===''||isNaN(rk)||rk<=0)return err('reportKg','Enter a weight above 0 kg');
          const iso=fromDMY(v.from);if(!iso)return err('from','Enter a date as DD/MM/YYYY');v.from=iso;
          err();
          const vals={toleranceG:tg,reportKg:rk,stageIds:[...sel]};const same=S.active('salePolicy').find(x=>(x.from||'')===v.from);
          UI.closeDrawer();
          UI.undoable(same?'Sale eligibility updated':'New version from '+fmt(v.from),()=>{if(same)Object.assign(same,vals);else S.add('salePolicy',Object.assign(vals,{from:v.from}));App.render();});
        };}});
  }
  function historyDrawer(coll){
    const st=states(coll),rows=S.active(coll).slice().sort((a,b)=>(b.from||'').localeCompare(a.from||''));
    const label=coll==='salePolicy'?'Sale eligibility':REG.R[coll].label;
    UI.drawer({head:DH('rotate',label,'History'),wide:1,cls:'idr',
      body:rows.length?`<div class="sltbl-wrap"><table class="sltbl"><thead><tr><th>Effective from</th><th>Scope</th><th>Value</th><th>Status</th></tr></thead><tbody>${rows.map(r=>`<tr>
        <td>${r.from?esc(fmt(r.from)):'<span class="muted">—</span>'}</td><td>${coll==='salePolicy'?'All':esc(scopeLine(coll,r))}</td>
        <td class="num">${coll==='salePrices'?inr(r.price)+(r.basis==='per_kg'?'/kg':'/animal'):coll==='saleMinWeights'?esc(r.minKg)+' kg':coll==='valuationRates'?inr(r.rate)+'/kg':esc(r.toleranceG)+' g · '+esc(r.reportKg)+' kg'}</td>
        <td>${stateTag(st[r.id])}</td></tr>`).join('')}</tbody></table></div>`:'<div class="empty">No versions</div>'});
  }
  Object.assign(window.A=window.A||{},{
    'sl-policy':()=>policyDrawer(),
    'sl-edit':el=>ruleDrawer(el.dataset.reg,el.dataset.id),
    'sl-hist':el=>historyDrawer(el.dataset.reg),
    'sl-grid':el=>{const k=el.dataset.reg,ids=S.active(k).map(r=>r.id),o=List.opts[k]||{};ids.length?Sheet.openEntry(k,ids):Sheet.openEntry(k,null,[o.defaults?o.defaults():{}]);}
  });
  /* ---------- connected outputs: Weighing operator verdict, Weighing and Sales module pages ---------- */
  /* no herd lookup on the capture path: species comes from the SOP, then species-wide minimum and margin */
  X.opVerdict=function(n,sop,r){
    if(!sop||sop.dept!=='Weighing'||n.unit!=='kg'||!/^weight$/i.test(String(n.label||'').trim()))return '';
    const spId=sop.speciesId||(sop.settings&&sop.settings.speciesId)||'sp_goat',raw=r.ans[n.id],w=raw===''||raw==null?NaN:Number(raw);
    const mw=current('saleMinWeights').find(x=>x.speciesId===spId&&!x.breedId&&!x.sexId),p=policy(),tol=+p.toleranceG||0,pill=(t,tone)=>`<span class="op-pill ${tone}">${esc(t)}</span>`;
    if(!mw)return '';
    const st=isNaN(w)?'':w>=mw.minKg?'ready':w>=mw.minKg-tol/1000?'margin':'below';
    const v=st==='ready'?pill('Sale-ready','ok'):st==='margin'?pill('Sale-ready · within '+tol+' g margin','ok'):st==='below'?pill('Below sale weight','mut'):'';
    return `<div class="op-row op-lims" data-salev="${st}">${v}${pill(REG.labelById('species',spId)+' sale weight '+mw.minKg+' kg · margin '+tol+' g','mut')}</div>`;
  };
  const kp=(l,v,sub)=>`<div class="kpi"><div class="lab">${esc(l)}</div><div class="val">${v}</div>${sub?`<div class="dl">${sub}</div>`:''}</div>`;
  const cfgLink=(l,h)=>`<a class="btn sm" href="${h}">${ic('settings')}${esc(l)}</a>`;
  X.weighingCard=function(){
    const {c,p,rows}=X.readiness(),cur=current('saleMinWeights'),mins=cur.filter(r=>!r.breedId&&!r.sexId),ov=cur.length-mins.length;
    return `<section class="card mt" data-salecard="weighing"><div class="hd lhd"><div class="ttl"><div class="eyebrow">Herd</div><h3>Sale readiness</h3></div><span class="sp"></span>${cfgLink('Sale eligibility','#/configuration/items/saleEligibility')}</div>
      <div class="grid g3 slkpi" style="padding-top:14px">${kp('In eligible stages',`<span data-rc="eligible">${c.eligible}</span>`,c.ineligible+' in other stages')}${kp('Sale-ready',`<span data-rc="ready">${c.ready}</span>`,esc(mins.map(m=>REG.labelById('species',m.speciesId)+' ≥ '+m.minKg+' kg').join(' · ')+(ov?' · +'+ov+' override'+(ov===1?'':'s'):''))+' · '+esc(marginNote(c.margin,p)))}${kp('Below sale weight',`<span data-rc="below">${c.below}</span>`,c.none?c.none+' not weighed or no minimum':'')}</div>
      <div class="twrap screen ltable"><table class="ltbl"><thead><tr><th>Species · stage</th><th class="num">Animals</th><th class="num">Sale-ready</th><th class="num">Within margin</th><th class="num">Below</th></tr></thead><tbody>${
        Object.entries(rows).sort().map(([k,g])=>`<tr><td data-label="Species · stage"><b>${esc(k.replace('|',' · '))}</b></td><td class="num" data-label="Animals">${g.n}</td><td class="num" data-label="Sale-ready">${g.ready}</td><td class="num" data-label="Within margin">${g.margin}</td><td class="num" data-label="Below">${g.below}</td></tr>`).join('')||'<tr><td colspan="5" class="muted">No animals in eligible stages</td></tr>'}</tbody></table></div></section>`;
  };
  function valuationOf(a){
    const st=S.get('stages',a.stageId),code=st?st.code:'',rates=current('valuationRates'),by=n=>rates.find(r=>r.name===n);
    const g=/^K[0-3]$/.test(code)?by(code):/^FAT/.test(code)?by('Fattening animals'):a.sexId==='sx_female'?by('Adult females'):a.sexId==='sx_male'?by('Adult males / bucks'):null;
    if(!g)return null;const w=g.assumedKg===''||g.assumedKg==null?Number(a.weight):Number(g.assumedKg);
    return isNaN(w)?null:{g,value:g.rate*w};
  }
  X.valueCard=function(){
    const an=S.active('animals'),R=X.readiness(),p=R.p;let tp=0,tv=0,np=0;
    const rows=an.map(a=>{const m=match('salePrices',a).rule,ss=R.by[a.id],val=valuationOf(a);
      const price=m?(m.basis==='per_animal'?m.price:m.price*Number(a.weight)):null;if(ss.state==='ready'&&price!=null&&!isNaN(price)){tp+=price;np++;}if(val)tv+=val.value;
      return [`<b class="mono">${esc(a.rfid.slice(-6))}</b>`,esc(REG.labelById('breeds',a.breedId)),esc(REG.labelById('sexes',a.sexId)),esc(REG.labelById('stages',a.stageId)),esc(a.weight),
        ss.state==='ready'?UI.tag('Sale-ready','ok')+(ss.margin?`<div class="muted small">within ${+p.toleranceG||0} g margin</div>`:''):ss.state==='below'?UI.tag('Below','mut'):ss.state==='ineligible'?'<span class="muted small">Not eligible stage</span>':'<span class="muted">—</span>',
        price!=null&&!isNaN(price)?`<span data-est>${inr(Math.round(price))}</span>`:'<span class="muted">No rule</span>',val?inr(Math.round(val.value)):'<span class="muted">—</span>'];});
    const H=[['Animal'],['Breed'],['Sex'],['Stage'],['Weight (kg)',1],['Sale-ready'],['Sale price',1],['Farm value',1]];
    return `<section class="card mt" data-salecard="value"><div class="hd lhd"><div class="ttl"><div class="eyebrow">Herd</div><h3>Value per animal <span class="cnt">${an.length}</span></h3></div><span class="sp"></span>${cfgLink('Sale price rules','#/configuration/items/salePrices')}${cfgLink('Valuation rates','#/configuration/items/valuationRates')}</div>
      <div class="grid g3 slkpi" style="padding-top:14px">${kp('Estimated sale value',`<span data-tot="price">${np?inr(Math.round(tp)):'—'}</span>`,np+' of '+R.c.ready+' sale-ready priced')}${kp('Sale-ready',`<span data-rc="ready">${R.c.ready}</span>`,R.c.eligible+' in eligible stages')}${kp('Farm value',`<span data-tot="value">${inr(Math.round(tv))}</span>`,an.length+' animals')}</div>
      <div class="twrap screen ltable"><table class="ltbl"><thead><tr>${H.map(h=>`<th class="${h[1]?'num':''}">${h[0]}</th>`).join('')}</tr></thead><tbody>${rows.map(r=>`<tr>${r.map((c,i)=>`<td class="${H[i][1]?'num':''}" data-label="${H[i][0]}">${c}</td>`).join('')}</tr>`).join('')}</tbody></table></div></section>`;
  };
  X.match=match;X.policy=policy;X.states=states;
  window.Sales=X;
})();
