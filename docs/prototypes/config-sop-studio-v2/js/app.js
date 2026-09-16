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
    {group:'Preventive Care',icon:'heart-pulse',leaves:[['Vaccination','#/vaccination'],['Live Drive Tracker','#/vaccination/live-tracker'],['Vaccination plan','#/vaccination/plan']]},
    {group:'Procurement',icon:'truck',leaves:[['Source Entry','#/procurement/source-entry'],['Vendors','#/procurement/vendors'],['Feed Purchases','#/procurement/feed-purchases'],['Animal purchases','#/procurement/animal-purchases'],['Procurement SOP','#/procurement/sops']]},
    {group:'Health',icon:'stethoscope',leaves:[['Health Analytics','#/health/analytics'],['Health Config','#/health/config']]},
    {group:'Others',icon:'edit-3',leaves:[['Milk Preparation','#/counts/milk-preparation'],['Milk SOP','#/milk/sops'],['Live Monitor','#/herd-signals'],['Audit Log','#/operations/audit'],['People / HRMS','#/people'],['Leave','#/leave']]},
    {group:'Configuration',icon:'settings',leaves:[['Items and settings','#/configuration/items'],['Work instructions','#/configuration/work-instructions']]}
  ];
  const SOP_DEPT={'#/counts/sops':'Counts','#/weighing/sops':'Weighing','#/feed/sops':'Feed','#/procurement/sops':'Procurement','#/milk/sops':'Milk'};

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
      else if(SOP_DEPT[h]&&S.active('sops').some(s=>s.dept===SOP_DEPT[h])){const n=NAV.find(x=>(x.leaves||[]).some(([,hh])=>hh===h));out={html:Work.listPage(SOP_DEPT[h],n?n.group:'')};}
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
  App.leave=function(hash,force){
    const d=!force&&SetupPage.dirty&&SetupPage.dirty();
    if(d){UI.toast('Unsaved changes',null,{label:'Discard',run:()=>{d.discard();App._allow=true;location.hash=hash;}});return;}
    if(force){const x=SetupPage.dirty&&SetupPage.dirty();if(x)x.discard();}
    App._allow=true; if(location.hash===hash)App.render(); else location.hash=hash;
  };
  window.addEventListener('hashchange',e=>{
    UI.closeMenu();UI.closePop();if(UI._drawer)UI.closeDrawer();
    if(!App._allow&&App._lastPath&&SetupPage.dirty){const d=SetupPage.dirty();
      if(d){const target=location.hash;history.replaceState(null,'',App._lastPath);UI.toast('Unsaved changes',null,{label:'Discard',run:()=>{d.discard();App._allow=true;location.hash=target;}});return;}}
    App._allow=false;App.render();});
  window.addEventListener('resize',()=>{if(UI._popCfg)UI._popRender();});

  window.App=App;
  try{if(localStorage.getItem('mesha.theme')==='light'){document.documentElement.classList.add('light');document.documentElement.classList.remove('dark');}}catch(e){}
  S.load();
  if(!location.hash)history.replaceState(null,'','#/configuration/items');
  App.render();
})();
