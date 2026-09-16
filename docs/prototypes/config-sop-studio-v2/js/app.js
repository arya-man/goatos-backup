/* Shell (topbar + sidebar), hash router, global event delegation */
(function(){
  const App={route:{path:'',parts:[],params:{}},ui:{open:{},park:'',rail:false}};
  const NAV=[
    {label:'Approvals',href:'#/approvals',icon:'gavel'},
    {label:'Verify',href:'#/verify',icon:'clipboard-check'},
    {label:'Tasks',href:'#/tasks',icon:'clipboard-list'},
    {label:'Work Board',href:'#/work-board',icon:'square-kanban'},
    {label:'Alerts',href:'#/alerts',icon:'tower-control'},
    {group:'Counts',icon:'bar-chart-3',leaves:[['Herd Analytics','#/counts/analytics'],['Counts Breakdown','#/counts/breakdown'],['Herd Operations SOP','#/counts/sops']]},
    {group:'Weighing',icon:'scale',leaves:[['ADG Analytics','#/weighing/analytics'],['Weighing SOP','#/weighing/sops']]},
    {group:'Sales',icon:'banknote',leaves:[['Summary','#/sales/sold'],['Farm value','#/sales/farm-value'],['Load wise','#/sales/loads'],['Market analytics','#/sales/market-analytics'],['Buyer analytics','#/sales/buyer-analytics'],['Vendors','#/sales/vendors'],['Sales Config','#/sales/config']]},
    {group:'Feed',icon:'wheat',leaves:[['Feed Config','#/feed/config'],['Feed Analytics','#/feed/analytics'],['Feed SOP','#/feed/sops']]},
    {group:'Preventive Care',icon:'heart-pulse',leaves:[['Vaccination','#/vaccination'],['Live Drive Tracker','#/vaccination/live-tracker'],['Vaccination plan','#/vaccination/plan'],['Preventive Care SOP','#/preventive-care/sops']]},
    {group:'Procurement',icon:'truck',leaves:[['Source Entry','#/procurement/source-entry'],['Vendors','#/procurement/vendors'],['Feed Purchases','#/procurement/feed-purchases'],['Animal purchases','#/procurement/animal-purchases'],['Procurement SOP','#/procurement/sops']]},
    {group:'Health',icon:'stethoscope',leaves:[['Health Analytics','#/health/analytics'],['Health Config','#/health/config'],['Health SOP','#/health/sops']]},
    {group:'Others',icon:'edit-3',leaves:[['Milk Preparation','#/counts/milk-preparation'],['Milk SOP','#/milk/sops'],['Live Monitor','#/herd-signals'],['Audit Log','#/operations/audit'],['People / HRMS','#/people'],['Leave','#/leave']]},
    {group:'Configuration',icon:'settings',leaves:[['Items and settings','#/configuration/items'],['Work instructions','#/configuration/work-instructions']]}
  ];
  const SOP_DEPT={'#/counts/sops':'Counts','#/weighing/sops':'Weighing','#/feed/sops':'Feed','#/procurement/sops':'Procurement','#/milk/sops':'Milk','#/health/sops':'Health','#/preventive-care/sops':'Preventive Care'};

  function activeHref(){
    const h=location.hash||'#/configuration/items';
    let best='';NAV.forEach(n=>{(n.leaves||[[n.label,n.href]]).forEach(([,href])=>{if((h===href||h.startsWith(href+'/'))&&href.length>best.length)best=href;});});
    return best;
  }
  function renderSide(){
    const act=activeHref();
    const html=NAV.map(n=>{
      if(!n.group)return `<a class="nav ${act===n.href?'on':''}" href="${n.href}">${ic(n.icon)}${esc(n.label)}</a>`;
      const has=n.leaves.some(([,h])=>h===act); const open=App.ui.open[n.group]!=null?App.ui.open[n.group]:has;
      return `<div><div class="ggrp ${open?'open':''}" role="button" aria-expanded="${open}" data-a="grp" data-g="${esc(n.group)}">${ic(n.icon)}${esc(n.group)}${ic('chevron-right','chev')}</div>
        <div class="subnav ${open?'open':''}">${n.leaves.map(([l,h])=>`<a class="leaf ${act===h?'on':''}" href="${h}">${esc(l)}</a>`).join('')}</div></div>`;
    }).join('')+`<div class="grow"></div><div class="sidefoot">Mesha · goat operating system</div>`;
    document.getElementById('side').innerHTML=html;
  }
  function renderTop(){
    document.querySelector('.hamb').innerHTML=ic('menu');
    const light=document.documentElement.classList.contains('light');
    document.getElementById('themebtn').innerHTML=ic(light?'moon':'sun');
    document.getElementById('bellbtn').innerHTML=ic('bell');
    document.getElementById('mebtn').innerHTML=`<span class="av">RT</span><span><span class="nm">Ravi Teja</span><span class="rl">CEO / CXO</span></span>${ic('chevron-right','',14)}`;
    const park=S.get('parks',App.ui.park);
    document.getElementById('parksel').innerHTML=`<button class="pscope" data-a="park-open">${ic('map-pin','',14)}<b>${esc(park?park.name:'All parks')}</b>${ic('chevron-down','',12)}</button>
      <div class="parkmenu ${App.ui.parkOpen?'on':''}"><div class="pm-label">Park scope</div><div class="pm-list">
      <div class="pm-item ${park?'':'on'}" data-a="park-pick" data-id=""><span class="pn">All parks</span></div>
      ${S.active('parks').map(p=>`<div class="pm-item ${App.ui.park===p.id?'on':''}" data-a="park-pick" data-id="${p.id}"><span class="pc">${esc(p.code||'')}</span><span class="pn">${esc(p.name)}</span></div>`).join('')}</div></div>`;
  }
  /* goatos-stg readback, queried read-only 2026-09-17 (health_protocol_versions, feed_ration_groups, feed_item_catalog,
     feed_session_templates, feed_shed_tags, feed_water_removal_config, protocol_versions/protocol_rules,
     vaccination_capacity_config, market_questions, market_survey_config). */
  const STG={
    health:{published:54,drafts:2,diseases:[['Abscesses',7],['Acidosis',2],['Anemia',4],['Arthritis',1],['Bloating',1],['Body Edema',1],['Diarrhea',3],['Dog Bite',4],['Ear tag cleaning',7],['Fever',3],['Fly strike',7],['Foot rot',7],['Fracture',13],['Heat Stress',1],['Horn Damage',10],['Jaundice',4],['Lumps',10],['Mastitis',3],['Milk fever',3],['Not Eating',3],['ORF',24],['Pinkeye',10],['Pregnancy toxemia',3],['Red Urine',4],['Skin Infections',5],['Udder Edema',7],['Wounds',10]]},
    feed:{shedTags:31,sessions:['Morning 50%','Evening 50%'],removalCutoff:'21:00',
      items:[['Dry Masoor Bhusa','active'],['UHT Milk','active'],['Mesha Kids Concentrate','active'],['Mesha Adult Concentrate','active']],retiredItems:13,
      rations:[['Anantapur Sheep','Anantapur Sheep'],['Beetal','Beetal/Sirohi'],['Beetal x Malai','Beetal/Sirohi'],['Beetal x Sojat','Beetal/Sirohi'],['Boer','Boer'],['Boer x Beetal','Beetal/Sirohi'],['Boer x Malai','Beetal/Sirohi'],['Boer x Sirohi','Beetal/Sirohi'],['Boer x Sojat','Beetal/Sirohi'],['Malai','Malai'],['Malai x Osmanabadi','Malai'],['Malai x Sojat','Sojat'],['Osmanabadi','Osmanabadi'],['Osmanabadi x Sojat','Sojat'],['Sirohi','Beetal/Sirohi'],['Sojat','Sojat']]},
    vacc:{version:'V9',label:'V9 screenshot rules restored: PPR 16w FMD HS 12w',publishedAt:'30/08/2026',draft:'V10',rules:25,capPerDay:200,maxShots:3,bufferDays:7,
      vaccines:[['Z1+Z3',['Kid 4w','Kid 7w','Adult dose 1','Adult dose 2 (+21d)','Every 182d']],['FMD',['Kid 12w','Adult','Every 274d']],['PPR',['Kid 16w','Adult','Every 1095d']],['HS',['Kid 12w','Adult','Every 365d']],['Blue Tongue',['Kid 16w','Kid 19w','Adult dose 1','Adult dose 2 (+21d)','Every 365d']],['Goat Pox',['Kid 16w','Adult','Every 365d']],['Sheep Pox',['Kid 16w','Adult','Every 365d']]]},
    sales:{callTime:'08:00',questions:['Goat live price','Sheep live price','Goat carcass price','Sheep carcass price','Goat offals price','Sheep offals price'],unit:'₹/kg'},
  };
  const kpi=(l,v,sub)=>`<div class="kpi"><div class="lab">${esc(l)}</div><div class="val">${v}</div>${sub?`<div class="dl">${esc(sub)}</div>`:''}</div>`;
  const tbl=(title,cnt,heads,rows)=>`<section class="card mt"><div class="hd lhd"><div class="ttl"><h3>${esc(title)} <span class="cnt">${cnt}</span></h3></div></div>
    <div class="twrap screen ltable"><table class="ltbl"><thead><tr>${heads.map(h=>`<th class="${h[1]?'num':''}">${esc(h[0])}</th>`).join('')}</tr></thead>
    <tbody>${rows.map(r=>`<tr>${r.map((c,i)=>`<td class="${heads[i][1]?"num":""}" data-label="${esc(heads[i][0])}">${c}</td>`).join('')}</tr>`).join('')}</tbody></table></div></section>`;
  const head=(grp,leaf)=>`<div class="phead"><div><div class="crumb">${esc(grp)} / <b>${esc(leaf)}</b></div><h1>${esc(leaf)}</h1></div></div>`;
  const MODULE_PAGES={
    '#/health/config':()=>head('Health','Health Config')+`<div class="grid g4 kpis">${kpi('Diseases',STG.health.diseases.length)}${kpi('Published protocols',STG.health.published,'Adult + kid')}${kpi('Drafts',STG.health.drafts)}${kpi('Longest course',Math.max(...STG.health.diseases.map(d=>d[1]))+'<small> days</small>')}</div>`
      +tbl('Treatment protocols',STG.health.diseases.length,[['Disease'],['Age bands'],['Version',1],['Course (days)',1]],STG.health.diseases.map(([n,d])=>[`<b>${esc(n)}</b>`,UI.tag('Adult','mut')+' '+UI.tag('Kid','mut'),'v1',d])),
    '#/feed/config':()=>head('Feed','Feed Config')+`<div class="grid g4 kpis">${kpi('Ration groups',new Set(STG.feed.rations.map(r=>r[1])).size,STG.feed.rations.length+' breeds')}${kpi('Feed items',STG.feed.items.length,STG.feed.retiredItems+' retired')}${kpi('Pen tags',STG.feed.shedTags)}${kpi('Sessions',STG.feed.sessions.length,STG.feed.sessions.join(' · '))}</div>`
      +tbl('Ration groups',STG.feed.rations.length,[['Breed'],['Ration group']],STG.feed.rations.map(([b,g])=>[`<b>${esc(b)}</b>`,esc(g)]))
      +tbl('Feed items',STG.feed.items.length,[['Item'],['Status']],STG.feed.items.map(([n,st])=>[`<b>${esc(n)}</b>`,UI.statusTag(st)])),
    '#/vaccination/plan':()=>{const v=STG.vacc;return head('Preventive Care','Vaccination plan')+`<div class="grid g4 kpis">${kpi('Published plan',v.version,v.publishedAt+' · '+v.draft+' draft')}${kpi('Vaccines',v.vaccines.length,v.rules+' rules')}${kpi('Animals per operator-day',v.capPerDay)}${kpi('Max shots per animal',v.maxShots,'Buffer '+v.bufferDays+' days')}</div>`
      +tbl('Vaccines · '+v.version,v.vaccines.length,[['Vaccine'],['Doses']],v.vaccines.map(([n,d])=>[`<b>${esc(n)}</b>`,d.map(x=>UI.tag(x,'mut')).join(' ')]))
;},
    '#/sales/config':()=>head('Sales','Sales Config')+`<div class="grid g4 kpis">${kpi('Market questions',STG.sales.questions.length)}${kpi('Daily market call',STG.sales.callTime)}</div>`
      +tbl('Market questions',STG.sales.questions.length,[['Question'],['Unit']],STG.sales.questions.map(q=>[`<b>${esc(q)}</b>`,esc(STG.sales.unit)]))
  };
  /* Procurement owns the one vendor register (admin-web /procurement/vendors); trucks on the same page */
  function vendorsPage(tab){
    const vs=S.all('vendors'),tr=S.all('trucks');
    const tabs=`<div class="tabs"><a class="${tab==='trucks'?'':'on'}" href="#/procurement/vendors">Vendors <span class="muted">${vs.filter(v=>v.status!=='archived').length}</span></a><a class="${tab==='trucks'?'on':''}" href="#/procurement/vendors/trucks">Trucks <span class="muted">${tr.filter(t=>t.status!=='archived').length}</span></a></div>`;
    const body=tab==='trucks'
      ?tbl('Trucks',tr.length,[['Number'],['Vendor'],['Capacity',1],['Status']],tr.map(t=>{const v=S.get('vendors',t.vendorId);return [`<b class="mono">${esc(t.number)}</b>`,esc(v?v.name:''),esc(t.capacity==null?'':t.capacity),UI.statusTag(t.status)];}))
      :tbl('Vendors',vs.length,[['Vendor'],['Supplies'],['City'],['Trucks',1],['Status']],vs.map(v=>[`<b>${esc(v.name)}</b>`,esc(v.supplies||''),esc(v.city||''),tr.filter(t=>t.vendorId===v.id).length,UI.statusTag(v.status)]));
    return `<div class="phead"><div><div class="crumb">Procurement / <b>Vendors</b></div><h1>Vendors</h1></div></div>${tabs}${body.replace('card mt','card')}`;
  }
  function placeholder(){
    const act=activeHref(); let grp='',leaf='';
    NAV.forEach(n=>{if(!n.group&&n.href===act)leaf=n.label;(n.leaves||[]).forEach(([l,h])=>{if(h===act){grp=n.group;leaf=l;}});});
    return `<div class="phead"><div>${grp?`<div class="crumb">${esc(grp)} / <b>${esc(leaf)}</b></div>`:''}<h1>${esc(leaf||'Mesha')}</h1></div></div><section class="card" style="min-height:260px"></section>`;
  }

  App.render=function(){
    const h=location.hash||'#/configuration/items';
    const parts=h.replace(/^#\/?/,'').split('?')[0].split('/').filter(Boolean);
    App.route={path:h,parts,params:{}};
    renderTop(); renderSide();
    const wrap=document.getElementById('wrap'), main=document.getElementById('main');
    const keepScroll=App._lastPath===h?main.scrollTop:0;
    let out={html:''};
    try{
      if(parts[0]==='configuration'&&parts[1]==='items')out=SetupPage.render(parts.slice(2));
      else if(parts[0]==='configuration'&&parts[1]==='work-instructions'){
        if(parts[2]==='master'&&parts[3]){App.route.params.id=parts[3];out={html:Master.page(parts[3],parts[4]),mount:r=>Master.mount(r,parts[3])};}
        else if(parts[2]){App.route.params.id=parts[2];const view=parts[3];
          out={html:Work.editorPage(parts[2],view),mount:r=>{
            if(view==='operator'){const box=r.querySelector('[data-meshaop]');const sop=S.get('sops',parts[2]);
              if(box&&window.MeshaOperator&&MeshaOperator.render){try{MeshaOperator.render(box,sop);}catch(e){console.error(e);box.innerHTML='<div class="empty">Operator view unavailable</div>';}}
              else if(box)box.innerHTML='<div class="empty">Operator view unavailable</div>';}
            else Work.mountEditor(r,parts[2]);}};}
        else out={html:Work.listPage()};
      }
      else if(SOP_DEPT[h]){const n=NAV.find(x=>(x.leaves||[]).some(([,hh])=>hh===h));out={html:Work.listPage(SOP_DEPT[h],n?n.group:'')};}
      else if(h==='#/procurement/vendors'||h==='#/procurement/vendors/trucks')out={html:vendorsPage(parts[2])};
      else if(MODULE_PAGES[h])out={html:MODULE_PAGES[h]()};
      else out={html:placeholder()};
    }catch(e){console.error(e);out={html:`<div class="phead"><h1>Something went wrong</h1></div><section class="card pad"><pre class="mono" style="white-space:pre-wrap">${esc(e.stack||e)}</pre></section>`};}
    wrap.innerHTML=out.html;
    if(out.mount)out.mount(wrap);
    if(App._lastPath===h)main.scrollTop=keepScroll; else main.scrollTop=0;
    App._lastPath=h;
  };
  App.renderCanvasOnly=function(){
    const p=App.route.parts; if(p[1]!=='work-instructions'||!p[2])return;
    const s=S.get('sops',p[2]); const cv=document.querySelector('[data-canvas]'); if(!s||!cv)return;
    cv.innerHTML=Flow.canvasHtml(s,Work.sel[s.id],window.MeshaOperator&&MeshaOperator.runMap?MeshaOperator.runMap(s):null);
  };

  Object.assign(A,{
    'grp'(el){const g=el.dataset.g;const cur=el.classList.contains('open');App.ui.open[g]=!cur;renderSide();},
    'nav-toggle'(){const l=document.getElementById('layout');if(window.innerWidth<=860){const on=!document.getElementById('side').classList.contains('open');document.getElementById('side').classList.toggle('open',on);document.querySelector('.navscrim').classList.toggle('on',on);}else l.classList.toggle('mnav');},
    'nav-close'(){document.getElementById('side').classList.remove('open');document.querySelector('.navscrim').classList.remove('on');},
    'theme'(){const r=document.documentElement;r.classList.toggle('light');r.classList.toggle('dark');try{localStorage.setItem('mesha.theme',r.classList.contains('light')?'light':'dark');}catch(e){}renderTop();},
    'park-open'(el,e){e.stopPropagation();App.ui.parkOpen=!App.ui.parkOpen;renderTop();},
    'park-pick'(el){App.ui.park=el.dataset.id;App.ui.parkOpen=false;renderTop();},
    'user-menu'(el){UI.menu(el,[
      {label:'Theme',icon:'sun',run:()=>A.theme()},
      {label:'Reset data',icon:'rotate',danger:true,run:()=>{S.reset();Object.keys(List.st).forEach(k=>delete List.st[k]);Sheet.cur=null;IO.state=null;if(window.MeshaOperator)S.all('sops').forEach(x=>MeshaOperator.reset(x.id));App._allow=true;App.render();UI.toast('Data reset');}}]);},
    'drawer-close'(){UI.closeDrawer();}
  });

  document.addEventListener('click',e=>{
    const el=e.target.closest('[data-a]');
    if(!e.target.closest('.parksel')&&App.ui.parkOpen){App.ui.parkOpen=false;renderTop();}
    if(!e.target.closest('#menu')&&!(el&&(el.dataset.a||'').includes('menu')))UI.closeMenu();
    if(e.target.closest('.side a')&&window.innerWidth<=860)A['nav-close']();
    if(!el)return;
    const fn=A[el.dataset.a]; if(!fn)return;
    if(el.tagName==='A'&&el.getAttribute('href')==='javascript:void 0')e.preventDefault();
    fn(el,e);
  });
  document.addEventListener('keydown',e=>{if(e.key==='Escape'){UI.closeMenu();UI.closePop();if(UI._drawer)UI.closeDrawer();}});
  /* unsaved-draft guard: Keep editing / Save and leave / Discard */
  function guard(d,target){
    UI.drawer({title:'Unsaved changes',body:'',
      foot:`<button class="btn" data-a="drawer-close">Keep editing</button><span class="sp"></span><button class="btn dngo" data-lg="discard">Discard</button>${d.save?'<button class="btn p" data-lg="save">Save and leave</button>':''}`,
      mount(dr){
        dr.querySelector('[data-lg="discard"]').onclick=()=>{UI.closeDrawer();d.discard();App._allow=true;if(location.hash===target)App.render();else location.hash=target;};
        const sv=dr.querySelector('[data-lg="save"]');if(sv)sv.onclick=()=>{UI.closeDrawer();d.save(target);};
      }});
  }
  App.leave=function(hash,force){
    const d=!force&&SetupPage.dirty&&SetupPage.dirty();
    if(d){guard(d,hash);return;}
    if(force){const x=SetupPage.dirty&&SetupPage.dirty();if(x)x.discard();}
    App._allow=true; if(location.hash===hash)App.render(); else location.hash=hash;
  };
  window.addEventListener('hashchange',e=>{
    UI.closeMenu();UI.closePop();if(UI._drawer)UI.closeDrawer();
    if(!App._allow&&App._lastPath&&SetupPage.dirty){const d=SetupPage.dirty();
      if(d){const target=location.hash;history.replaceState(null,'',App._lastPath);guard(d,target);return;}}
    App._allow=false;App.render();});
  window.addEventListener('beforeunload',e=>{const d=SetupPage.dirty&&SetupPage.dirty();if(d){e.preventDefault();e.returnValue='';}});
  window.addEventListener('resize',()=>{if(UI._popCfg)UI._popRender();});

  window.App=App;
  try{if(localStorage.getItem('mesha.theme')==='light'){document.documentElement.classList.add('light');document.documentElement.classList.remove('dark');}}catch(e){}
  S.load();
  if(!location.hash)history.replaceState(null,'','#/configuration/items');
  App.render();
})();
