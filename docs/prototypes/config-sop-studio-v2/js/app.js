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
  const SOP_OF={counts:['Herd Operations SOP','#/counts/sops'],weighing:['Weighing SOP','#/weighing/sops'],feed:['Feed SOP','#/feed/sops'],vaccination:['Preventive Care SOP','#/preventive-care/sops'],procurement:['Procurement SOP','#/procurement/sops'],health:['Health SOP','#/health/sops'],milk:['Milk SOP','#/milk/sops']};
  const SOP_DEPT={'#/counts/sops':'Counts','#/weighing/sops':'Weighing','#/feed/sops':'Feed','#/procurement/sops':'Procurement','#/milk/sops':'Milk','#/health/sops':'Health','#/preventive-care/sops':'Preventive Care'};

  function activeHref(){
    const h=location.hash||'#/configuration/items';
    let best='';NAV.forEach(n=>{(n.leaves||[[n.label,n.href]]).forEach(([,href])=>{if((h===href||h.startsWith(href+'/'))&&href.length>best.length)best=href;});});
    return best;
  }
  function renderSide(){
    const act=activeHref();
    const html=NAV.map(n=>{
      if(!n.group)return `<a class="nav ${act===n.href?'on':''}" href="${n.href}" title="${esc(n.label)}" aria-label="${esc(n.label)}">${ic(n.icon)}${esc(n.label)}</a>`;
      const has=n.leaves.some(([,h])=>h===act); const open=App.ui.open[n.group]!=null?App.ui.open[n.group]:has;
      return `<div><div class="ggrp ${open?'open':''} ${has?'on':''}" role="button" tabindex="0" title="${esc(n.group)}" aria-label="${esc(n.group)}" aria-expanded="${open}" data-a="grp" data-g="${esc(n.group)}">${ic(n.icon)}${esc(n.group)}${ic('chevron-right','chev')}</div>
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
    vacc:{version:'V9',label:'V9 screenshot rules restored: PPR 16w FMD HS 12w',publishedAt:'31/08/2026' /* IST (published 30/08 UTC evening) */,draft:'V10',rules:25,capPerDay:200,maxShots:3,bufferDays:7,
      vaccines:[['Z1+Z3',['Kid 4w','Kid 7w','Adult dose 1','Adult dose 2 (+21d)','Every 182d']],['FMD',['Kid 12w','Adult','Every 274d']],['PPR',['Kid 16w','Adult','Every 1095d']],['HS',['Kid 12w','Adult','Every 365d']],['Blue Tongue',['Kid 16w','Kid 19w','Adult dose 1','Adult dose 2 (+21d)','Every 365d']],['Goat Pox',['Kid 16w','Adult','Every 365d']],['Sheep Pox',['Kid 16w','Adult','Every 365d']]]},
    asOf:'17/09/2026',
    vendors:{total:674,procurement:313,sales:361,types:[['Butcher',183,1],['Farmer',109,1],['Sheep Agent',89],['Agent',57,1],['Transport Agent',56],['Manure Agent',37],['Feed Agent',31],['Pellet Factory',13],['Goats Agent',13],['Sheep Stockist',12],['Company',12,1],['Goat Stockist',10],['Breeding Agent',7],['Labor Agent',6],['Goat Farm',5],['UHT Milk Supplier',4],['Feed Stockist',4],['Insurance',4],['Veterinary Accessories',4],['Test Lab',4],['Chain Link Mesh Contractor',3],['Solar Light Supplier',3],['Flooring Mat',2],['Grain Supplier',2],['Steel Material Supplier',2],['Vet Doctor',1],['Grass Cutter',1]]},
    /* per nav leaf: read-only counts, SELECT-only against goatos-stg on 17/09/2026 */
    /* procurement_loads, all accepted intake; linked = procurement_load_goats rows */
    loads:[['22/06/2026',63,63,'23/06/2026'],['12/06/2026',77,75,'13/06/2026'],['01/06/2026',78,77,'02/06/2026'],['26/05/2026',70,66,'27/05/2026'],['11/05/2026',67,63,'12/05/2026'],['13/11/2025',100,0,'14/11/2025'],['23/10/2025',76,1,'24/10/2025'],['21/10/2025',70,3,'22/10/2025']],
    mod:{
      '#/approvals':{k:[['Shifting awaiting approval',2],['Birth / death pending',0],['Approved',55,'51 shifting · 3 death · 1 birth'],['Rejected',19,'Shifting']]},
      '#/verify':{k:[['Pending videos','2,677'],['Feed packing','1,447'],['Feed distribution',769],['Weighing',122,'Proof · fasting · animal · pen']],
        t:['Pending by category',[['Category'],['Pending',1]],[['Feed packing','1,447'],['Feed distribution',769],['Feed wastage',134],['Feed transport',96],['Weighing proof',75],['Milk feeding',52],['Weighing fasting',39],['Milk preparation',34],['Shifting move',11],['Hoof trimming',6],['Weighing animal',5],['Deworming',4],['Weighing pen',3],['Birth evidence',1],['Death evidence',1]].map(([a,b])=>[`<b>${a}</b>`,b])]},
      '#/tasks':{k:[['Vaccination queued',537],['Vaccination in review',14,'11 needs review · 3 submitted'],['Pen visits open',26,'13 delayed'],['Toxin tests open',11,'2 awaiting review']]},
      '#/work-board':{k:[['Reconcile cards open',25],['Birth tracks open',1],['Shifting authorized',13,'2 pending'],['Feed removal assigned',1]]},
      '#/alerts':{k:[['Notifications, 7 days','35,183'],['Alert rules',0],['Pen visits delayed',13],['Milk feeds not submitted',162,'Last 30 days']]},
      '#/counts/analytics':{k:[['Live animals','1,521','CBE 805 · CPT 716'],['Goats',697,'578 F · 119 M'],['Sheep',824,'407 F · 417 M'],['Sold',145,'3 dead · 14 inactive']]},
      '#/counts/breakdown':{k:[['Non-Pregnant',759],['F2-Male',465],['F2-Female',187],['Buck',38]],
        t:['Animals by stage',[['Stage'],['Animals',1]],[['Non-Pregnant',759],['F2-Male',465],['F2-Female',187],['Buck',38],['K3',34],['ICU-Kid',24],['K2',8],['Mother',5],['K0',1]].map(([a,b])=>[`<b>${a}</b>`,b])]},
      '#/weighing/analytics':{k:[['Animal weighs, 30 days','1,761'],['Pen weighs, 30 days',36],['Animals weighed, 60 days',580],['Tasks open',5,'1 in progress · 4 published']]},
      '#/sales/sold':{k:[['Closed deals',71],['Animals sold',691],['Sales value','₹88.5L'],['Failed deals',1,'46 animals']]},
      '#/sales/farm-value':{k:[['Live animals','1,521'],['Coimbatore',805],['Channapatna',716],['Parks',2]]},
      '#/sales/loads':{k:[['Loads',8],['Animals bought',601],['Status','Accepted intake']],t:['Loads',[['Purchased'],['Expected',1],['Animals linked',1],['Arrived']],null]},
      '#/sales/market-analytics':{k:[['Market benchmarks',10,'Updated 19/08/2026'],['Daily market call','08:00'],['Market questions',6]]},
      '#/sales/buyer-analytics':{k:[['Buyers',26],['Closed deals',71],['Animals sold',691]]},
      '#/sales/vendors':{k:[['Sales-side vendors',361],['Butcher',183],['Farmer',109],['Agent · Company',69,'57 · 12']]},
      '#/feed/analytics':{k:[['Distributions done, 30 days','4,984'],['Awaiting verification',769],['Rework',66],['Feed loads reached',238,'1 purchased, in transit']]},
      '#/vaccination':{k:[['Drive days ahead',12],['Pen assignments ahead',106],['Animals ahead','1,652'],['Next drive','22/09/2026','4 animals']]},
      '#/vaccination/live-tracker':{k:[['Queued sheds',537],['Submitted',3],['Needs review',11],['Accepted',10]]},
      '#/procurement/source-entry':{k:[['Loads',8],['Animals expected',601],['Status','Accepted intake']]},
      '#/procurement/feed-purchases':{k:[['Loads',239],['Reached',238],['In transit',1],['Toxin tests open',11]]},
      '#/procurement/animal-purchases':{k:[['Loads',8],['Animals',601],['Purchase loads open',2,'80 animals expected']],t:['Loads',[['Purchased'],['Expected',1],['Animals linked',1],['Arrived']],null]},
      '#/health/analytics':{k:[['Health cases',0],['Published protocols',54],['Diseases',27],['ICU-Kid animals',24]]},
      '#/counts/milk-preparation':{k:[['Preparations, 30 days',48],['Completed',13],['Awaiting verification',35],['Feeding sessions pending',78,'Awaiting verification']],sop:['Milk SOP','#/milk/sops']},
      '#/herd-signals':{k:[['Tags',19],['Low movement',14],['Quiet',3],['Not moving',2]]},
      '#/operations/audit':{k:[['Events, 24 h','8,214'],['Events, 7 days','71,269']],
        t:['Events by record, 7 days',[['Record'],['Events',1]],[['Calendar notifications','35,228'],['Device check-ins','24,085'],['Feed packing','3,025'],['Feed distribution','2,551'],['Preventive care animals','2,306'],['Sign-ins','1,686'],['Weighing','744'],['Feed wastage',602],['Preventive care tasks',236],['Feed transport',210],['Purchase candidates',123],['Pen visits',71]].map(([a,b])=>[`<b>${a}</b>`,b])]},
      '#/people':{k:[['Active people',41],['Inactive',1]],
        t:['Active people by role',[['Role'],['People',1]],[['Operator',30],['CEO / CXO',6],['Verifier',1],['Health Director',1],['Feed Director',1],['Preventive Care Director',1],['Other',1]].map(([a,b])=>[`<b>${a}</b>`,b])]},
      '#/leave':{k:[['Leave requests',0]]}
    },
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
  /* Procurement owns the one vendor register (admin-web /procurement/vendors); counts are goatos-stg procurement_vendors */
  function vendorsPage(){
    const vs=S.all('vendors'),v=STG.vendors;
    return head('Procurement','Vendors')+`<div class="grid g4 kpis">${kpi('Register',v.total,STG.asOf)}${kpi('Procurement side',v.procurement,v.types.filter(t=>!t[2]).length+' types')}${kpi('Sales side',v.sales,'Sales › Vendors')}${kpi('Top type',v.types[0][1],v.types[0][0])}</div>`
      +(vs.length?tbl('Vendors',vs.length,[['Vendor'],['Type'],['City'],['Status']],vs.map(x=>[`<b>${esc(x.name)}</b>`,esc(x.recordType||x.supplies||''),esc(x.city||''),UI.statusTag(x.status)])):'')
      +tbl('Record types',v.types.length,[['Record type'],['Side'],['Vendors',1]],v.types.map(([t,n,sale])=>[`<b>${esc(t)}</b>`,sale?UI.tag('Sales','teal'):UI.tag('Procurement','mut'),n]))
      +sopRow('#/procurement/vendors');
  }
  function sopRow(href){const m=STG.mod[href];const sop=m&&m.sop||SOP_OF[href.split('/')[1]]||['Work instructions','#/configuration/work-instructions'];
    return `<section class="card mt"><a class="linkrow" href="${sop[1]}">${ic('clipboard-list')}<b>${esc(sop[0])}</b><span class="sp"></span>${ic('chevron-right')}</a></section>`;}
  function placeholder(){
    const act=activeHref(); let grp='',leaf='';
    NAV.forEach(n=>{if(!n.group&&n.href===act)leaf=n.label;(n.leaves||[]).forEach(([l,h])=>{if(h===act){grp=n.group;leaf=l;}});});
    const m=STG.mod[act];
    const kp=m?`<div class="grid g4 kpis">${m.k.map(([l,v,sub])=>kpi(l,v,sub)).join('')}</div>`:'';
    const rows=m&&m.t&&!m.t[2]?tbl(m.t[0],STG.loads.length,m.t[1],STG.loads.map(([d,e,g,a])=>[`<b>${d}</b>`,e,g,a])):m&&m.t?tbl(m.t[0],m.t[2].length,m.t[1],m.t[2]):'';
    return `<div class="phead"><div>${grp?`<div class="crumb">${esc(grp)} / <b>${esc(leaf)}</b></div>`:''}<h1>${esc(leaf||'Mesha')}</h1></div></div>${kp}${rows}${act?sopRow(act):''}`;
  }

  App.render=function(){
    const h=location.hash||'#/configuration/items';
    const parts=h.replace(/^#\/?/,'').split('?')[0].split('/').filter(Boolean);
    App.route={path:h,parts,params:{}};
    railMode(); renderTop(); renderSide();
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
      else if(h==='#/procurement/vendors/trucks'){location.replace('#/procurement/vendors');return;}
      else if(h==='#/procurement/vendors')out={html:vendorsPage()};
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
    'grp'(el){const g=el.dataset.g;
      if(document.getElementById('layout').classList.contains('rail-icons')){const n=NAV.find(x=>x.group===g);const act=activeHref();
        UI.menu(el,n.leaves.map(([l,h])=>({label:(h===act?'• ':'')+l,run:()=>{location.hash=h;}})));
        const m=document.getElementById('menu'),r=el.getBoundingClientRect();if(m){m.style.left=(r.right+6)+'px';m.style.top=Math.max(8,Math.min(r.top,window.innerHeight-m.offsetHeight-8))+'px';}return;}
      const cur=el.classList.contains('open');App.ui.open[g]=!cur;renderSide();},
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
  /* Esc closes the innermost layer only: picker popup, then menu, then drawer */
  let escPop=false; /* captured before the field's own popKey closes the popup */
  window.addEventListener('keydown',e=>{if(e.key==='Escape'){const pop=document.getElementById('cbpop');escPop=!!(UI._popCfg||(pop&&pop.style.display!=='none'&&pop.offsetParent!==null));}},true);
  document.addEventListener('keydown',e=>{if(e.key!=='Escape')return;
    const m=document.getElementById('menu');
    if(escPop||UI._popCfg){escPop=false;UI.closePop();e.stopPropagation();return;}
    if(m&&m.offsetParent!==null&&m.innerHTML.trim()){UI.closeMenu();e.stopPropagation();return;}
    if(UI._drawer)UI.closeDrawer();});
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
  /* 861-1179px: icon-only nav rail with tooltips; phones keep the drawer */
  /* 861-1179px everywhere; SOP / master editors also collapse up to 1366px so the canvas gets the room */
  function railMode(){const w=window.innerWidth,l=document.getElementById('layout');const ed=/^#\/configuration\/work-instructions\/[^/?]+/.test(location.hash||'');l.classList.toggle('rail-icons',w>860&&(w<1180||(ed&&w<=1366)));}
  window.addEventListener('resize',()=>{railMode();if(UI._popCfg)UI._popRender();});

  window.App=App;
  try{if(localStorage.getItem('mesha.theme')==='light'){document.documentElement.classList.add('light');document.documentElement.classList.remove('dark');}}catch(e){}
  railMode();
  S.load();
  if(!location.hash)history.replaceState(null,'','#/configuration/items');
  App.render();
})();
