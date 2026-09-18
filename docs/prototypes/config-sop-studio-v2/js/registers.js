/* Register definitions shared by lists, forms, grids, import and export */
(function(){
  const norm=s=>String(s==null?'':s).trim().toLowerCase().replace(/\s+/g,' ');
  const ST=['active','archived'];
  const ref=(k,label,refc,attr,extra)=>Object.assign({k,label,type:'ref',ref:refc,attr},extra||{});
  const txt=(k,label,extra)=>Object.assign({k,label,type:'text',attr:k},extra||{});
  const num=(k,label,extra)=>Object.assign({k,label,type:'num',attr:k,w:'num'},extra||{});
  const en=(k,label,opts,extra)=>Object.assign({k,label,type:'enum',opts,attr:k},extra||{});
  /* enum values are stored as codes; people see and may type human-case labels */
  const ENUM_WORDS={any:'Any',cxo:'CEO / CXO',ceo_cxo:'CEO / CXO',am:'Assistant manager',assistant_manager:'Assistant manager',yes_no:'Yes / no',multiselect:'Multi-select',growth_cohort:'Growth cohort',per_kg:'INR per kg live weight',per_animal:'INR per animal'};
  const enumLabel=o=>{o=String(o==null?'':o);if(!o)return '';if(ENUM_WORDS[o])return ENUM_WORDS[o];if(o!==o.toLowerCase())return o;const t=o.replace(/_/g,' ');return t[0].toUpperCase()+t.slice(1);};
  const gradeLabel=g=>{g=String(g==null?'':g);return (window.GRADE_LABELS&&window.GRADE_LABELS[g])||enumLabel(g);};
  const enumValue=(c,raw)=>{const n=norm(raw);return c.opts.find(o=>norm(o)===n||norm(enumLabel(o))===n);};
  /* animal identifiers compare ignoring case and spaces */
  /* switches are stored as true/false; legacy 'yes'/'Yes'/'true' still read as on */
  const isOn=x=>x===true||/^(yes|true|y|1)$/i.test(String(x==null?'':x).trim());
  const sw=(k,label,cap)=>Object.assign(en(k,label,['no','yes'],{cap}),{blankNo:1,bool:1,derive:r=>isOn(r[k])?'yes':'no'});
  const idNorm=s=>String(s==null?'':s).toLowerCase().replace(/\s+/g,'');

  const R={
    farms:{label:'Farms',one:'Farm',coll:'farms',icon:'map-pin',
      cols:[txt('code','Code',{req:1,w:'num'}),txt('name','Name',{req:1}),en('kind','Type',['core','holding','contract'])],
      key:v=>norm(v.code)},
    parks:{label:'Parks',one:'Park',coll:'parks',icon:'map-pin',
      cols:[ref('farm','Farm','farms','farmId'),txt('name','Name',{req:1}),txt('code','Code',{w:'num'})],
      key:v=>norm(v.farm)+'|'+norm(v.name)},
    pens:{label:'Pens',one:'Pen',coll:'pens',icon:'layers',
      cols:[ref('park','Park','parks','parkId',{req:1}),txt('name','Pen',{req:1}),num('capacity','Capacity',{min:0}),
        ref('stage','Stage','stages','stageId'),en('sex','Sex',['mixed','female','male']),
        Object.assign(en('hasIcu','ICU',['no','yes']),{bool:1,derive:r=>r.hasIcu===true||r.hasIcu==='yes'?'yes':'no'}),ref('lifecycle','Lifecycle status','stages','lifecycleStatus')],
      key:v=>norm(v.park)+'|'+norm(v.name)},
    partitions:{label:'Partitions',one:'Partition',coll:'partitions',icon:'layers',
      cols:[Object.assign(ref('park','Park','parks',null,{virtual:true}),{derive:r=>{const p=S.get('pens',r.penId);return p?p.parkId:'';}}),
        ref('pen','Pen','pens','penId',{req:1,scope:{col:'park',attr:'parkId'}}),txt('name','Partition',{req:1}),num('capacity','Capacity',{min:0})],
      key:v=>norm(v.park)+'|'+norm(v.pen)+'|'+norm(v.name)},
    species:{label:'Species',one:'Species',coll:'species',icon:'layers',
      cols:[txt('name','Name',{req:1}),txt('code','Code',{w:'num'})],key:v=>norm(v.name)},
    breeds:{label:'Breeds',one:'Breed',coll:'breeds',
      cols:[ref('species','Species','species','speciesId',{req:1}),txt('name','Breed',{req:1})],
      key:v=>norm(v.species)+'|'+norm(v.name)},
    sexes:{label:'Sexes',one:'Sex',coll:'sexes',
      cols:[txt('name','Sex',{req:1}),txt('code','Code',{w:'num'})],
      key:v=>norm(v.name)},
    stages:{label:'Lifecycle stages',one:'Stage',coll:'stages',
      cols:[ref('species','Species','species','speciesId',{req:1}),txt('code','Code',{req:1,w:'num'}),txt('name','Stage',{req:1}),
        num('fromD','From (days)',{min:0,ph:'Any'}),num('toD','To (days)',{min:0,ph:'Any'}),en('sex','Sex',['any','female','male'])],
      key:v=>norm(v.species)+'|'+norm(v.code),
      check:(v,iss)=>{if(v.fromD!==''&&v.toD!==''&&v.fromD!=null&&v.toD!=null&&!isNaN(v.fromD)&&!isNaN(v.toD)&&Number(v.toD)<Number(v.fromD))iss('toD','err','Before From');}},
    tags:{label:'Shed tags',one:'Shed tag',coll:'tags',
      cols:[ref('species','Species','species','speciesId',{blankLabel:'All species'}),txt('name','Name',{req:1}),en('kind','Kind',['tag','group'])],
      key:v=>norm(v.species)+'|'+norm(v.name)+'|'+norm(v.kind||'tag')},
    healthStates:{label:'Health states',one:'Health state',coll:'healthStates',
      cols:[txt('name','Name',{req:1,w:'wide'}),en('group','Group',['Milk Kids','Fattening Kids','Adults']),num('fromD','From (days)'),num('toD','To (days)')],key:v=>norm(v.name)},
    animals:{label:'Animals',one:'Animal',coll:'animals',icon:'list-checks',
      cols:[txt('rfid','RFID',{req:1,w:'wide',idTag:1}),txt('tag2','Second tag',{idTag:1}),
        ref('species','Species','species','speciesId',{req:1}),ref('breed','Breed','breeds','breedId',{scope:{col:'species',attr:'speciesId'}}),
        ref('sex','Sex','sexes','sexId',{req:1}),ref('stage','Stage','stages','stageId',{scope:{col:'species',attr:'speciesId'}}),
        Object.assign(txt('dob','Date of birth'),{type:'date'}),num('weight','Weight (kg)',{min:0}),
        ref('park','Park','parks','parkId',{req:1}),ref('pen','Pen','pens','penId',{req:1,scope:{col:'park',attr:'parkId'}}),
        ref('partition','Partition','partitions','partitionId',{scope:{col:'pen',attr:'penId'}}),
        Object.assign(ref('tags','Tags','tags','tagIds',{scope:{col:'species',attr:'speciesId',allowBlank:true}}),{type:'multi'})],
      key:v=>idNorm(v.rfid),keyCol:'rfid'},
    categories:{label:'Categories',one:'Category',coll:'categories',
      cols:[{k:'path',label:'Category path',type:'path',req:1,w:'wide'}],key:v=>norm(v.path)},
    items:{label:'Items',one:'Item',coll:'items',icon:'folder',
      cols:[txt('name','Item',{req:1,w:'wide'}),{k:'category',label:'Category',type:'path',attr:'categoryId',w:'wide'},txt('unit','Unit',{w:'num'}),
        Object.assign(en('depts','Departments',DEPTS),{type:'multienum'}),
        en('route','Route',['','IM','IV','SC','Oral','Topical'],{kinds:['medicine']}),txt('strength','Strength',{kinds:['medicine']}),
        num('withdrawalDays','Withdrawal days',{min:0,kinds:['medicine']}),en('prescription','Prescription needed',['','yes','no'],{kinds:['medicine']}),
        txt('disease','Disease covered',{kinds:['vaccine']}),num('doseMl','Dose (ml)',{min:0,kinds:['vaccine']}),
        en('vaccineType','Live / killed',['','live','killed'],{kinds:['vaccine']}),num('boosterDays','Booster gap (days)',{min:0,kinds:['vaccine']}),
        num('dryMatter','Dry matter %',{min:0,kinds:['feed']}),num('protein','Protein %',{min:0,kinds:['feed']}),
        sw('trackStock','Track stock','stock'),sw('serialized','Tracked by serial numbers','stock'),num('reorderAt','Reorder at',{min:0,cap:'stock'}),
        ref('stockRole','Stock alert role','roles','stockRoleId',{cap:'stock'}),
        sw('trackExpiry','Track expiry','expiry'),
        num('expiryAlertDays','Alert days before',{min:0,cap:'expiry'}),ref('expiryRole','Expiry alert role','roles','expiryRoleId',{cap:'expiry'}),
        sw('trackCheck','Recurring check','maint'),
        num('checkEvery','Check every (days)',{min:0,cap:'maint'}),ref('checkRole','Check role','roles','checkRoleId',{cap:'maint'}),
        en('proof','Photo/video proof',['','photo','video','none'],{cap:'maint'})],
      key:v=>norm(v.name),
      check:(v,iss,existing,opts)=>{
        const strict=opts&&opts.strict;const cur=existing?toRow('items',existing):{};
        const get=k=>String(v[k]!==undefined?v[k]:(cur[k]||'')).trim();
        const cat=get('category');
        const kindOfPath=p=>{const id=findPath(p)||findPath(String(p).split('>')[0].trim());return itemKindOfCat(id);};
        const kind=cat?kindOfPath(cat):(existing?itemKindOfCat(existing.categoryId):'general');
        if(existing&&v.category!==undefined&&itemKindOfCat(existing.categoryId)!==kind)iss('category','err','Cannot move between catalogues');
        /* required per kind: a blank that was already blank on an existing record is incomplete, not an import error */
        const need=(k,msg)=>{if(get(k))return;const was=existing&&!String(cur[k]||'').trim();iss(k,was&&!strict?'warn':'err',was&&!strict?'Incomplete':(msg||'Required'));};
        if(ITEM_SUB_REQ[kind]&&!cat.includes('>'))need('category','Subcategory required');
        (ITEM_REQ[kind]||[]).forEach(k=>need(k));
        const own=OWNER_DEPT[kind];if(own&&v.depts!==undefined&&!String(v.depts).split(/[;,]/).map(norm).includes(norm(own)))iss('depts','info',own+' is always on for '+KINDS[kind]);
        const on=k=>get(k).toLowerCase()==='yes';
        Object.entries(ITEM_CAPS).forEach(([sw,c])=>{
          if(c.general&&kind!=='general'){if(on(sw))iss(sw,'err','Not for this catalogue');return;}
          if(on(sw))c.req.forEach(k=>need(k));
          else c.fields.forEach(k=>{const col=R.items.cols.find(x=>x.k===k);if(v[k]!==undefined&&String(v[k]).trim()&&!(col&&col.bool&&!isOn(v[k])))iss(k,'err','Turn on '+R.items.cols.find(x=>x.k===sw).label);});
        });}},
    /* stock is transactional (inventory lots + movements), never an item field; this register is the stock receipt import */
    lots:{label:'Stock receipts',one:'Lot',coll:'lots',icon:'download',
      cols:[ref('item','Item','items','itemId',{req:1,w:'wide'}),txt('lotNo','Lot no.',{req:1}),ref('park','Park','parks','parkId',{req:1}),
        Object.assign(num('qty','Quantity',{req:1,min:0}),{reqUnless:v=>!!serialCount(String(v.serialFrom||'').trim(),String(v.serialTo||'').trim())}),txt('serialFrom','Serial from'),txt('serialTo','Serial to'),Object.assign(txt('expiresOn','Expires on'),{type:'date'}),Object.assign(txt('receivedOn','Received on'),{type:'date'}),
        ref('vendor','Vendor','vendors','vendorId')],
      key:v=>norm(v.item)+'|'+norm(v.lotNo),
      check:(v,iss,existing,opts,rows,idx)=>{
        const it=findRef('lots',R.lots.cols[0],v.item,v).rec;if(!it)return;
        if(!isOn(it.trackStock)){iss('item','err','Track stock is off for this item');return;}
        if(existing||S.all('lots').some(l=>l.itemId===it.id&&norm(l.lotNo)===norm(v.lotNo)))iss('lotNo','err','Lot already received');
        const sf=String(v.serialFrom||'').trim(),st=String(v.serialTo||'').trim();
        if(!isOn(it.serialized)){if(sf||st)iss(sf?'serialFrom':'serialTo','err','Item is not tracked by serial numbers');return;}
        if(!sf)iss('serialFrom','err','Required');if(!st)iss('serialTo','err','Required');if(!sf||!st)return;
        const c=serialCount(sf,st);if(!c){iss('serialTo','err','Range must share a prefix and increase');return;}
        if(String(v.qty==null?'':v.qty).trim()!==''&&+v.qty!==c)iss('qty','err','Must equal range size '+c);
        const hit=serialOverlap(it.id,sf,st);if(hit){iss('serialFrom','err','Overlaps lot '+hit.lotNo);return;}
        (rows||[]).slice(0,idx).forEach(r=>{const o=r.v;if(norm(o.item)===norm(v.item)&&rangesOverlap(o.serialFrom,o.serialTo,sf,st))iss('serialFrom','err','Overlaps row above');});
      }},
    roles:{label:'Roles',one:'Role',coll:'roles',
      cols:[txt('name','Role',{req:1,w:'wide'}),en('grade','Grade',['','ceo_cxo','director','head','manager','am'])],key:v=>norm(v.name)},
    people:{label:'People',one:'Person',coll:'people',icon:'user',
      cols:[txt('name','Name',{req:1,w:'wide'}),ref('role','Role','roles','roleId',{req:1}),Object.assign(ref('parks','Parks','parks','parkIds'),{type:'multi',blankLabel:'All parks'}),txt('email','Email',{unique:1}),txt('phone','Phone')],
      key:v=>norm(v.name)},
    /* Vendors & trucks are owned by Procurement (prod admin-web /procurement/vendors); not a register here. */
    statusDefs:{label:'Status definitions',one:'Status',coll:'statusDefs',
      cols:[en('axis','Axis',['lifecycle','reproductive','health','growth_cohort','management'],{req:1}),txt('code','Key',{req:1,w:'num'}),txt('name','Status',{req:1}),num('days','Expected days',{min:0})],
      key:v=>norm(v.axis)+'|'+norm(v.code)},
    exitReasons:{label:'Exit reasons',one:'Exit reason',coll:'exitReasons',cols:[txt('name','Exit reason',{req:1,w:'wide'})],key:v=>norm(v.name)},
    purposes:{label:'Animal purposes',one:'Purpose',coll:'purposes',cols:[txt('name','Purpose',{req:1,w:'wide'})],key:v=>norm(v.name)},
    movementReasons:{label:'Movement reasons',one:'Movement reason',coll:'movementReasons',cols:[txt('name','Reason',{req:1,w:'wide'})],key:v=>norm(v.name)},
    weightBands:{label:'Weight bands',one:'Weight band',coll:'weightBands',
      cols:[ref('species','Species','species','speciesId',{blankLabel:'All species'}),txt('name','Band',{req:1}),num('fromKg','From (kg)',{min:0}),num('toKg','To (kg)',{min:0})],
      key:v=>norm(v.species)+'|'+norm(v.name)},
    /* Diseases, symptoms and ration groups are owned by Health Config (#/health/config) and Feed Config (#/feed/config); not registers here. */
    sopCategories:{label:'SOP categories',one:'SOP category',coll:'sopCategories',cols:[txt('code','Key',{req:1,w:'num'}),txt('name','Category',{req:1,w:'wide'})],key:v=>norm(v.code)},
    taskTypes:{label:'Task types',one:'Task type',coll:'taskTypes',cols:[txt('code','Key',{req:1,w:'wide'}),txt('name','Task type',{req:1,w:'wide'}),en('answer','Answer',['none','yes_no','select','multiselect','number','text'])],key:v=>norm(v.code)},
    /* business rules that used to be code constants */
    saleProducts:{label:'Sale product types',one:'Sale product type',coll:'saleProducts',
      cols:[txt('name','Product',{req:1,w:'wide'}),ref('species','Species','species','speciesId',{blankLabel:'Any species'})],key:v=>norm(v.name)},
    costKinds:{label:'Cost kinds',one:'Cost kind',coll:'costKinds',
      cols:[txt('code','Key',{req:1,w:'num'}),txt('name','Cost kind',{req:1,w:'wide'})],key:v=>norm(v.code)},
    identifierPolicies:{label:'Identifier policies',one:'Identifier',coll:'identifierPolicies',
      cols:[txt('code','Key',{req:1,w:'wide'}),txt('name','Identifier',{req:1}),en('primaryAllowed','Primary allowed',['yes','no']),en('autoLink','Auto link',['yes','no']),txt('policy','Policy',{w:'wide'})],
      key:v=>norm(v.code)},
    /* job titles; RBAC roles stay in Roles and are never repeated here */
    designations:{label:'Designations',one:'Designation',coll:'designations',
      cols:[txt('code','Key',{req:1,w:'num'}),txt('name','Designation',{req:1,w:'wide'}),en('grade','Grade',['','cxo','director','head','manager','am'])],
      key:v=>norm(v.code)},
    approvalChains:{label:'Approval chains',one:'Approval chain',coll:'approvalChains',
      cols:[en('dept','Department',DEPTS,{req:1}),txt('name','Request',{req:1,w:'wide'}),{k:'steps',label:'Approvers (in order)',type:'steps',attr:'steps',req:1,w:'wide'}],
      key:v=>norm(v.dept)+'|'+norm(v.name)},
    settings:{label:'Business settings',one:'Setting',coll:'settings',
      cols:[en('dept','Department',DEPTS,{req:1}),txt('name','Setting',{req:1,w:'wide'}),txt('value','Value',{req:1,w:'num'}),txt('unit','Unit',{w:'num'})],
      key:v=>norm(v.dept)+'|'+norm(v.name)},
    /* Sales: each row is one dated version of a scope; the latest version on or before today is current */
    salePrices:{label:'Sale price rules',one:'Sale price rule',coll:'salePrices',
      cols:[ref('species','Species','species','speciesId',{req:1}),ref('breed','Breed','breeds','breedId',{scope:{col:'species',attr:'speciesId'},blankLabel:'Any'}),
        ref('sex','Sex','sexes','sexId',{blankLabel:'Any'}),ref('stage','Stage','stages','stageId',{scope:{col:'species',attr:'speciesId'},blankLabel:'Any'}),
        en('basis','Price basis',['per_kg','per_animal'],{req:1}),num('price','Price (INR)',{req:1,min:0}),Object.assign(txt('from','Effective from',{req:1}),{type:'date'})],
      key:v=>[v.species,v.breed,v.sex,v.stage,v.from].map(norm).join('|')},
    saleMinWeights:{label:'Minimum sale weight',one:'Minimum sale weight',coll:'saleMinWeights',
      cols:[ref('species','Species','species','speciesId',{req:1}),ref('breed','Breed','breeds','breedId',{scope:{col:'species',attr:'speciesId'},blankLabel:'Any'}),
        ref('sex','Sex','sexes','sexId',{blankLabel:'Any'}),num('minKg','Minimum (kg)',{req:1,min:0}),Object.assign(txt('from','Effective from'),{type:'date'})],
      check:(v,iss)=>{if(v.minKg!==''&&v.minKg!=null&&!isNaN(v.minKg)&&Number(v.minKg)<=0)iss('minKg','err','Above 0');},
      key:v=>[v.species,v.breed,v.sex,v.from].map(norm).join('|')},
    valuationRates:{label:'Valuation rates',one:'Valuation rate',coll:'valuationRates',
      cols:[txt('name','Group',{req:1}),txt('stages','Stages'),num('rate','Rate (INR/kg)',{req:1,min:0}),num('assumedKg','Assumed weight (kg)',{min:0}),Object.assign(txt('from','Effective from'),{type:'date'})],
      key:v=>norm(v.name)+'|'+norm(v.from)}
  };
  /* approval chains are the ONE approver source. A step is a role, a designation or a named person:
     "Counts Approver (per person); Park Head; Operator 01" <-> [{role,perPerson}|{designation}|{person}] */
  const baseName=s=>norm(String(s||'').replace(/\s*\(.*\)\s*$/,''));
  const cleanRole=n=>String(n||'').replace(/\s*\(.*\)\s*$/,'');
  const recKey=r=>r.key||r.code;
  function stepInfo(s){
    if(s.role){const r=S.all('roles').find(x=>x.key===s.role);return {kind:'role',key:s.role,id:r&&r.id,label:(r?cleanRole(r.name):enumLabel(s.role))+(s.perPerson?' (per person)':''),perPerson:!!s.perPerson};}
    if(s.person){const p=S.get('people',s.person);return {kind:'person',key:s.person,id:s.person,label:p?p.name:s.person};}
    const d=S.all('designations').find(x=>recKey(x)===s.designation);return {kind:'designation',key:s.designation,id:d&&d.id,label:d?d.name:enumLabel(s.designation)};
  }
  const stepLabel=s=>stepInfo(s).label;
  /* picker options for a step: roles, then designations, then people (same label is offered once) */
  function stepOptions(){
    const out=[],seen=new Set();const add=(label,sub)=>{const n=norm(label);if(seen.has(n))return;seen.add(n);out.push({id:label,label,sub});};
    S.active('roles').forEach(r=>add(cleanRole(r.name),'Role'));
    S.active('designations').forEach(d=>add(d.name,'Designation'));
    S.active('people').forEach(p=>add(p.name,'Person'));
    return out;
  }
  function parseSteps(raw){
    const steps=[],bad=[];
    String(raw||'').split(/;/).map(t=>t.trim()).filter(Boolean).forEach(t=>{
      const per=/\(per person\)\s*$/i.test(t); const n=norm(t.replace(/\(per person\)\s*$/i,''));
      const r=S.active('roles').find(x=>norm(x.key)===n||baseName(x.name)===n||norm(x.name)===n);
      if(r)return steps.push(per?{role:r.key,perPerson:true}:{role:r.key});
      const d=S.active('designations').find(x=>norm(recKey(x))===n||norm(x.name)===n);
      if(d)return steps.push({designation:recKey(d)});
      const p=S.active('people').find(x=>norm(x.name)===n);
      if(p)return steps.push({person:p.id});
      bad.push(t);
    });
    return {steps,bad};
  }
  /* approvers for a chain: id, "Dept|Request" or request name -> ordered [{kind,key,id,label,perPerson}] */
  function approversFor(chainKey){
    const n=norm(chainKey);const all=S.active('approvalChains');
    const ch=S.get('approvalChains',chainKey)||all.find(c=>norm(c.dept)+'|'+norm(c.name)===n)||all.find(c=>norm(c.name)===n);
    return ch?(ch.steps||[]).map(stepInfo):[];
  }
  /* stored-state shape for the business-rule registers (flat rows, one approver source); idempotent */
  function migrate(st){
    if(!st)return;
    const idp=st.identifierPolicies;
    if(Array.isArray(idp)&&idp.some(p=>Array.isArray(p.types))){
      st.identifierPolicies=[].concat(...idp.map(p=>Array.isArray(p.types)?p.types.map(t=>({id:'idp_'+t.code,code:t.code,name:t.name,primaryAllowed:t.primaryAllowed?'yes':'no',autoLink:t.autoLink?'yes':'no',policy:p.code+(p.version?' v'+p.version:''),status:p.status||'active'})):[p]));
    }
    /* the old flat "Approvers" list folds into approval chains (department + request + role step) */
    if(Array.isArray(st.approvers)){
      st.approvalChains=st.approvalChains||[];
      st.approvers.forEach(a=>{
        const role=(st.roles||[]).find(r=>r.id===a.roleId);if(!role)return;
        let ch=st.approvalChains.find(c=>norm(c.dept)===norm(a.dept)&&norm(c.name)===norm(a.step));
        if(!ch){ch={id:'apc_'+a.id,dept:a.dept,name:a.step,steps:[],status:a.status||'active'};st.approvalChains.push(ch);}
        if(!(ch.steps||[]).some(x=>x.role===role.key||x.designation===role.key))ch.steps=(ch.steps||[]).concat([{role:role.key}]);
      });
      delete st.approvers;
    }
    /* designations and roles are separate lists: restore seed designations an older build dropped */
    if(Array.isArray(st.designations)&&window.buildSeed&&window.seedRefs&&!st._dsgRestored){
      try{const seed=window.buildSeed();window.seedRefs(seed);(seed.designations||[]).forEach(d=>{if(!st.designations.some(x=>recKey(x)===recKey(d)))st.designations.push(d);});}catch(e){}
      st._dsgRestored=1;
    }
  }
  if(window.S&&!S._regMigrate){S._regMigrate=1;const load=S.load,reset=S.reset;
    const owners=st=>{(st.items||[]).forEach(i=>{delete i.stockQty;delete i.expiresOn;['trackStock','serialized','trackExpiry','trackCheck'].forEach(k=>{if(i[k]!==undefined)i[k]=isOn(i[k]);});if(ownerDept(i.categoryId))i.depts=withOwner(i.depts,i.categoryId);});};
    S.load=function(){const r=load.apply(S,arguments);migrate(S.state);owners(S.state);S.save();return S.state||r;};
    S.reset=function(){const r=reset.apply(S,arguments);migrate(S.state);owners(S.state);S.save();return r;};}

  /* optional registers exist only while their seed collection does */
  const OPTIONAL=[];
  try{const st=window.buildSeed?window.buildSeed():null;if(st&&window.seedRefs)window.seedRefs(st);
    if(st)OPTIONAL.forEach(k=>{if(!Array.isArray(st[R[k].coll]))delete R[k];});}catch(e){}
  Object.keys(R).forEach(k=>{R[k].key_=k; if(!R[k].statuses)R[k].statuses=ST;});

  /* catalogue kinds: a category's kind is its root's kind (medicine | vaccine | feed are fixed catalogues; everything else general) */
  const KINDS={medicine:'Medicines',vaccine:'Vaccines',feed:'Feed items',general:'List'};
  /* one map for the item form and import validation */
  const ITEM_REQ={medicine:['unit','route','strength','withdrawalDays','prescription'],vaccine:['disease','doseMl','vaccineType','boosterDays'],feed:['unit'],general:[]};
  const ITEM_SUB_REQ={medicine:true};
  /* fixed catalogues own a department that is always on for their items */
  const OWNER_DEPT={medicine:'Health',vaccine:'Preventive Care',feed:'Feed'};
  const ownerDept=catId=>OWNER_DEPT[itemKindOfCat(catId)]||'';
  function withOwner(depts,catId){const o=ownerDept(catId);const d=(depts||[]).filter(x=>x!==o);return o?[o].concat(d):d;}
  /* stock: lots carry the current balance; movements are the history (receive, consume, adjust, transfer, expire) */
  const today=()=>new Date().toISOString().slice(0,10);
  function stockOf(itemId,parkId){const lots=S.active('lots').filter(l=>l.itemId===itemId&&+l.qty>0&&(!parkId||l.parkId===parkId));
    const exp=lots.map(l=>l.expiresOn).filter(Boolean).sort()[0]||'';return {qty:lots.reduce((a,l)=>a+(+l.qty||0),0),expiresOn:exp,lots:lots.length};}
  /* reorder level: item default plus optional per-park overrides; with no park it is the sum over parks */
  const lvl=x=>x===''||x==null||isNaN(+x)?null:+x;
  function reorderLevel(item,parkId){if(!item)return null;const by=item.reorderByPark||{};
    if(parkId)return lvl(by[parkId])!=null?lvl(by[parkId]):lvl(item.reorderAt);
    const ps=S.active('parks');if(!ps.length)return lvl(item.reorderAt);let any=false,t=0;ps.forEach(p=>{const x=reorderLevel(item,p.id);if(x!=null){any=true;t+=x;}});return any?t:null;}
  function lowStock(item,parkId){const l=reorderLevel(item,parkId);return l!=null&&stockOf(item.id,parkId).qty<=l;}
  function move(kind,lot,qty,extra){S.all('stockMoves').push(Object.assign({id:S.uid('mov'),kind,lotId:lot.id,itemId:lot.itemId,parkId:lot.parkId,qty,on:today()},extra||{}));}
  /* serial range "SF-048-00001" .. "SF-048-00500" → 500; a serial lot holds the contiguous range serialFrom..serialTo (size = qty) */
  const sp=x=>{const m=/^(.*?)(\d+)$/.exec(String(x==null?'':x).trim());return m?{p:m[1],n:+m[2],w:m[2].length}:null;};
  function serialCount(a,b){const m=sp(a),n=sp(b);if(!m||!n||m.p!==n.p)return null;const c=n.n-m.n+1;return c>0?c:null;}
  function serialAt(x,off){const a=sp(x);return a?a.p+String(a.n+off).padStart(a.w,'0'):'';}
  function rangesOverlap(a1,b1,a2,b2){const x1=sp(a1),y1=sp(b1),x2=sp(a2),y2=sp(b2);if(!x1||!y1||!x2||!y2||x1.p!==x2.p)return false;return x1.n<=y2.n&&x2.n<=y1.n;}
  const isSerialLot=l=>!!(l&&l.serialTo&&sp(l.serialTo));
  /* every range ever received for the item (split and merged lots included) */
  function serialOverlap(itemId,from,to,exceptId){return S.all('lots').find(l=>l.itemId===itemId&&l.id!==exceptId&&(l.rangeFrom||l.serialFrom)&&(l.rangeTo||l.serialTo)&&rangesOverlap(l.rangeFrom||l.serialFrom,l.rangeTo||l.serialTo,from,to))||null;}
  const serialMax=l=>isSerialLot(l)?serialCount(l.rangeFrom||l.serialFrom,l.serialTo)||+l.qty:null;
  function setQty(l,q){q=+q;if(isSerialLot(l))l.serialFrom=serialAt(l.serialTo,-(q-1));l.qty=q;}
  function receive(o){if(String(o.qty==null?'':o.qty).trim()===''&&o.serialFrom&&o.serialTo)o=Object.assign({},o,{qty:serialCount(o.serialFrom,o.serialTo)});const l=S.add('lots',{itemId:o.itemId,lotNo:o.lotNo,parkId:o.parkId,qty:+o.qty,serialFrom:o.serialFrom||'',serialTo:o.serialTo||'',rangeFrom:o.serialFrom||'',rangeTo:o.serialTo||'',expiresOn:o.expiresOn||'',receivedOn:o.receivedOn||today(),vendorId:o.vendorId||''});move('receive',l,+o.qty);return l;}
  function adjust(lotId,newQty,reason){const l=S.get('lots',lotId);if(!l)return false;const mx=serialMax(l);if(mx!=null&&+newQty>mx)return false;const d=+newQty-(+l.qty);setQty(l,newQty);move(reason==='Used'?'consume':'adjust',l,d,{reason:reason||''});return true;}
  function writeOff(lotId){const l=S.get('lots',lotId);if(!l)return;move('expire',l,-(+l.qty),{reason:'Expired'});setQty(l,0);}
  /* SOP / vaccination usage: first-expiry-first-out from the park's lots */
  function consume(itemId,qty,parkId){let left=+qty;S.active('lots').filter(l=>l.itemId===itemId&&(!parkId||l.parkId===parkId)&&+l.qty>0)
      .sort((a,b)=>(a.expiresOn||'9999').localeCompare(b.expiresOn||'9999')).forEach(l=>{if(left<=0)return;const t=Math.min(left,+l.qty);setQty(l,+l.qty-t);left-=t;move('consume',l,-t);});return +qty-left;}
  /* transfer moves qty (a serial lot moves the sub-range from its start) and merges into the same lot no. at the destination */
  function transfer(lotId,toParkId,qty){const l=S.get('lots',lotId);qty=+qty;if(!l||qty<=0||qty>+l.qty||toParkId===l.parkId)return null;
    const ser=isSerialLot(l),mf=ser?l.serialFrom:'',mt=ser?serialAt(l.serialFrom,qty-1):'';
    setQty(l,+l.qty-qty);if(ser)l.rangeFrom=l.serialFrom;
    const same=S.active('lots').filter(x=>x.itemId===l.itemId&&x.parkId===toParkId&&norm(x.lotNo)===norm(l.lotNo));
    let n=null;
    if(!ser)n=same[0]||null;
    else n=same.find(x=>+x.qty>0&&isSerialLot(x)&&(serialAt(x.serialTo,1)===mf||serialAt(mt,1)===x.serialFrom))||null;
    if(n){if(!ser)n.qty=+n.qty+qty;else{if(serialAt(n.serialTo,1)===mf){n.serialTo=mt;n.rangeTo=mt;}else{n.serialFrom=mf;n.rangeFrom=mf;}n.qty=+n.qty+qty;}}
    else n=S.add('lots',{itemId:l.itemId,lotNo:l.lotNo,parkId:toParkId,qty,serialFrom:mf,serialTo:mt,rangeFrom:mf,rangeTo:mt,expiresOn:l.expiresOn,receivedOn:l.receivedOn,vendorId:l.vendorId||''});
    move('transfer_out',l,-qty,{toParkId});move('transfer_in',n,+qty,{fromParkId:l.parkId});return n;}
  const ITEM_CAPS={trackStock:{fields:['serialized','reorderAt','stockRole'],req:['reorderAt','stockRole']},
    trackExpiry:{fields:['expiryAlertDays','expiryRole'],req:['expiryAlertDays','expiryRole']},
    trackCheck:{fields:['checkEvery','checkRole','proof'],req:['checkEvery','checkRole'],general:true}};
  function itemMissing(rec){if(!rec)return [];const row=toRow('items',rec);const kind=itemKindOfCat(rec.categoryId);const blank=k=>!String(row[k]==null?'':row[k]).trim();
    const out=(ITEM_REQ[kind]||[]).filter(blank);if(ITEM_SUB_REQ[kind]&&!String(row.category).includes('>'))out.push('category');
    Object.entries(ITEM_CAPS).forEach(([sw,c])=>{if(isOn(rec[sw])&&!(c.general&&kind!=='general'))c.req.filter(blank).forEach(k=>out.push(k));});return out;}
  function catRoot(id){let c=S.get('categories',id),g=0;while(c&&c.parentId&&g++<20){const p=S.get('categories',c.parentId);if(!p)break;c=p;}return c||null;}
  function itemKindOfCat(id){const r=catRoot(id);return r&&r.kind&&r.kind!=='general'?r.kind:'general';}
  function catSubtree(id){const out=new Set([id]);let grew=true;while(grew){grew=false;S.all('categories').forEach(c=>{if(out.has(c.parentId)&&!out.has(c.id)){out.add(c.id);grew=true;}});}return out;}
  /* where an item (or category) is used: SOP questions picking from its category, SOP steps naming it, published module rules */
  function itemUses(itemId,catId,activeOnly){
    const it=itemId?S.get('items',itemId):null; const cid=catId||(it&&it.categoryId)||'';
    const out=[];
    const hits=nodes=>(nodes||[]).filter(n=>(itemId&&n.itemId===itemId)||(n.answer==='ref'&&n.refColl==='items'&&(
        itemId?(!n.refCat||catSubtree(n.refCat).has(cid)):(n.refCat&&(catSubtree(cid).has(n.refCat)||catSubtree(n.refCat).has(cid))))));
    (activeOnly?S.active('sops'):S.all('sops')).forEach(s=>{
      const vs=s.versions||[];const pub=vs.length?hits(vs[vs.length-1].nodes):[];const hit=hits(s.nodes);
      /* published = the live version uses it; a draft edit alone never blocks archive */
      if(hit.length||pub.length)out.push({type:'sop',label:s.title,sub:(pub.length?pub:hit).map(n=>n.label).join(', '),href:'#/configuration/work-instructions/'+s.id,published:pub.length>0});
    });
    const items=itemId?[it].filter(Boolean):S.active('items').filter(x=>catSubtree(cid).has(x.categoryId));
    if(items.some(x=>x.plan))out.push({type:'rule',label:'Vaccination plan',sub:items.filter(x=>x.plan).map(x=>x.plan).join(', '),href:'#/vaccination/plan',published:true});
    if(items.some(x=>x.feedConfig))out.push({type:'rule',label:'Feed Config',href:'#/feed/config',published:true});
    if(items.some(x=>+x.protocols))out.push({type:'rule',label:'Health protocols',sub:items.reduce((a,x)=>a+(+x.protocols||0),0)+' protocols',href:'#/health/config',published:true});
    return out;
  }

  /* labels */
  function catPath(id){const out=[];let c=S.get('categories',id),g=0;while(c&&g++<20){out.unshift(c.name);c=S.get('categories',c.parentId);}return out.join(' > ');}
  function label(coll,rec){
    if(!rec)return '';
    if(coll==='categories')return catPath(rec.id);
    if(coll==='partitions'){const p=S.get('pens',rec.penId);return rec.name;}
    if(coll==='pens')return rec.displayName||rec.name||'';
    return rec.name||rec.number||rec.code||rec.rfid||'';
  }
  function fullLabel(coll,rec){
    if(!rec)return '';
    if(coll==='partitions'){const p=S.get('pens',rec.penId);return (p?label('pens',p)+' - ':'')+rec.name;}
    if(coll==='pens'){const p=S.get('parks',rec.parkId);return label('pens',rec)+(p?' · '+p.name:'');}
    if(coll==='breeds'||coll==='stages'){const s=S.get('species',rec.speciesId);return label(coll,rec)+(s?' · '+s.name:'');}
    return label(coll,rec);
  }
  const labelById=(coll,id)=>label(coll,S.get(coll,id));

  function cellValue(reg,col,rec){
    const v=col.derive?col.derive(rec):(col.attr?rec[col.attr]:rec[col.k]);
    if(col.type==='ref')return labelById(col.ref,v);
    if(col.type==='multi')return (v||[]).map(id=>labelById(col.ref,id)).filter(Boolean).join('; ');
    if(col.type==='path')return reg.coll==='categories'?catPath(rec.id):(v?catPath(v):'');
    if(col.type==='multienum')return (v||[]).map(enumLabel).join('; ');
    if(col.type==='steps')return (v||[]).map(stepLabel).join('; ');
    if(col.type==='enum')return enumLabel(v);
        return v==null?'':String(v);
  }
  function toRow(regKey,rec){const reg=R[regKey],o={};reg.cols.forEach(c=>o[c.k]=cellValue(reg,c,rec));o.status=rec.status||'active';return o;}

  /* candidate lookup for a ref column given row values */
  function scopeId(regKey,col,row){
    if(!col.scope)return undefined;
    const pc=R[regKey].cols.find(c=>c.k===col.scope.col);
    if(!pc||!String(row[pc.k]||'').trim())return undefined;
    const r=findRef(regKey,pc,row[pc.k],row);
    return r.rec?r.rec.id:null; // null = parent unresolved
  }
  function candidates(regKey,col,row,withArchived){
    let list=withArchived?S.all(col.ref):S.active(col.ref);
    const sid=scopeId(regKey,col,row||{});
    if(sid===null)return col.scope&&col.scope.allowBlank?list.filter(x=>!x[col.scope.attr]):[];
    if(sid!==undefined)list=list.filter(x=>x[col.scope.attr]===sid||(col.scope.allowBlank&&!x[col.scope.attr]));
    return list;
  }
  function matches(col,list,n,row){
    let hits=list.filter(x=>norm(label(col.ref,x))===n||(x.displayName&&norm(x.displayName)===n)||(col.ref!=='parks'&&norm(x.code)===n)||norm(x.number)===n);
    if(!hits.length&&col.ref==='parks')hits=list.filter(x=>norm(x.code)===n);
    if(!hits.length){hits=list.filter(x=>(x.aliases||[]).some(a=>norm(a)===n));
      if(hits.length>1&&col.ref==='stages'&&row){const sx=norm(row.sex);const bySex=hits.filter(x=>x.sex===sx||(sx==='female'&&/female/i.test(x.name))||(sx==='male'&&/\bmale/i.test(x.name)&&!/female/i.test(x.name)));if(bySex.length===1)hits=bySex;}}
    if(!hits.length&&col.ref==='stages'&&row&&/^(f2|fattening)$/.test(n)){const sx=norm(row.sex);hits=list.filter(x=>norm(x.name)===(sx==='female'?'fattening female':sx==='male'?'fattening male':'__'));}
    if(!hits.length&&col.ref==='partitions')hits=list.filter(x=>norm(fullLabel('partitions',x))===n);
    return hits;
  }
  function findRef(regKey,col,value,row){
    const n=norm(value); if(!n)return {rec:null,blank:true};
    if(col.type==='path'){const id=findPath(value);return {rec:id?S.get('categories',id):null};}
    let hits=matches(col,candidates(regKey,col,row||{}),n,row);
    if(hits.length>1&&col.scope&&col.scope.allowBlank){const sid=scopeId(regKey,col,row||{});const exact=hits.filter(x=>x[col.scope.attr]===sid);if(exact.length===1)hits=exact;}
    if(hits.length>1&&!col.scope&&hits.every(x=>norm(label(col.ref,x))===norm(label(col.ref,hits[0]))&&x.speciesId!==undefined&&x.parkId===undefined))hits=[hits[0]];
    if(hits.length>1)return {rec:null,ambiguous:hits.length};
    if(!hits.length){const arch=matches(col,candidates(regKey,col,row||{},true).filter(x=>x.status==='archived'),n);if(arch.length===1)return {rec:null,archived:arch[0]};}
    return {rec:hits[0]||null};
  }
  function findPath(path){
    const parts=String(path).split('>').map(s=>s.trim()).filter(Boolean); let parent='';let cur=null;
    for(const p of parts){cur=S.active('categories').find(c=>(c.parentId||'')===parent&&norm(c.name)===norm(p)); if(!cur)return null; parent=cur.id;}
    return cur?cur.id:null;
  }
  function ensurePath(path){
    const parts=String(path).split('>').map(s=>s.trim()).filter(Boolean); let parent='';let made=0;
    for(const p of parts){let cur=S.active('categories').find(c=>(c.parentId||'')===parent&&norm(c.name)===norm(p)); if(!cur){cur=S.add('categories',{name:p,parentId:parent});made++;} parent=cur.id;}
    return {id:parent,made};
  }
  /* create a referenced record; parentId forces the scope parent */
  function createRef(regKey,col,text,row,parentId){
    text=String(text).trim(); if(!text)return null;
    if(col.type==='path')return ensurePath(text).id;
    const attrs={};
    if(col.scope){
      const pc=R[regKey].cols.find(c=>c.k===col.scope.col);
      let pid=parentId!==undefined?parentId:scopeId(regKey,col,row||{});
      if(pid===null&&pc)pid=createRef(regKey,pc,row[pc.k],row);
      if(pid)attrs[col.scope.attr]=pid;
    }
    const c=col.ref;
    const arch=matches(col,S.all(c).filter(x=>x.status==='archived'&&(!col.scope||!attrs[col.scope.attr]||x[col.scope.attr]===attrs[col.scope.attr])),norm(text));
    if(arch.length===1){arch[0].status='active';return arch[0].id;}
    if(c==='stages'){attrs.code=text.replace(/\s+/g,'').toUpperCase().slice(0,10);attrs.name=text;attrs.sex='any';attrs.fromD='';attrs.toD='';}
    else if(c==='farms'){attrs.code=text.replace(/\s+/g,'').toUpperCase().slice(0,4);attrs.name=text;}
    else if(c==='parks'){attrs.name=text;attrs.code='';}
    else if(c==='tags'){attrs.name=text;attrs.kind='tag';attrs.speciesId=attrs.speciesId||'';}
    else if(c==='roles'){attrs.name=text;attrs.grade='';}
    else attrs.name=text;
    return S.add(c,attrs).id;
  }
  /* inline-create choices for a picker: one per possible parent when the parent is not chosen */
  /* a new master value must look like a name: at least one letter, not a number or formula */
  const nameLike=t=>{t=String(t==null?'':t).trim();return /\p{L}/u.test(t)&&!/^[=+@-]/.test(t)&&!/^\d+(\.\d+)?e\+?\d+$/i.test(t);};
  function createChoices(regKey,col,row,text){
    text=String(text||'').trim(); if(!text||col.ref==='sexes'||col.type==='enum'||col.type==='multienum'||!nameLike(text))return [];
    const refCol=Object.assign({},col,{type:col.type==='path'?'path':'ref'});
    if(col.type==='path')return [{label:'Create “'+text+'”',run:()=>createRef(regKey,refCol,text,row)}];
    if(!col.scope&&(col.ref==='stages'||col.ref==='breeds'))return S.active('species').map(sp=>({label:'Create “'+text+'” · '+sp.name,run:()=>{const id=createRef(regKey,refCol,text,row);const r=S.get(col.ref,id);if(r)r.speciesId=sp.id;return id;}}));
    if(col.scope){
      const pc=R[regKey].cols.find(c=>c.k===col.scope.col);
      const raw=String(row[col.scope.col]||'').trim();
      if(!raw){
        const parents=S.active(pc.ref).slice(0,8);
        const out=parents.map(p=>({label:'Create “'+text+'” · '+label(pc.ref,p),run:()=>createRef(regKey,refCol,text,row,p.id)}));
        if(col.scope.allowBlank)out.unshift({label:'Create “'+text+'” · '+(col.blankLabel||'All'),run:()=>createRef(regKey,refCol,text,row,'')});
        return out;
      }
      const pid=scopeId(regKey,col,row);
      return [{label:'Create “'+text+'” · '+raw,run:()=>createRef(regKey,refCol,text,row)}];
    }
    return [{label:'Create “'+text+'”',run:()=>createRef(regKey,refCol,text,row)}];
  }

  /* references to a record from any register (for delete guards) */
  function usage(coll,id){
    const out=[];
    Object.keys(R).forEach(k=>{const reg=R[k];reg.cols.forEach(c=>{
      if(c.virtual||!c.attr)return;
      if(c.ref===coll&&(c.type==='ref'||c.type==='multi')){const n=S.all(reg.coll).filter(x=>{const v=x[c.attr];return Array.isArray(v)?v.includes(id):v===id;}).length;if(n)out.push({label:reg.label,n});}
      if(coll==='categories'&&c.type==='path'&&c.attr){const n=S.all(reg.coll).filter(x=>x[c.attr]===id).length;if(n)out.push({label:reg.label,n});}
    });});
    if(coll==='categories'){const n=S.all('categories').filter(x=>x.parentId===id).length;if(n)out.push({label:'Subcategories',n});}
    if(coll==='items'){const x=itemUses(id);if(x.length)out.push({label:x.map(y=>y.label).join(', '),n:x.length});}
    if(coll==='roles'||coll==='settings'){const n=S.all('sops').filter(s=>s.nodes.some(nd=>nd.roleId===id||nd.itemId===id||nd.setting===id||nd.everySetting===id||nd.forSetting===id)).length;if(n)out.push({label:'SOPs',n});}
    if(coll==='designations'||coll==='roles'||coll==='people'){const n=chainUsers(coll,id).length;if(n)out.push({label:'Approval chains',n});}
    if(coll==='sops'){const n=S.all('masters').filter(m=>m.stages.some(s=>s.sopId===id)).length+S.all('sops').filter(s=>s.nodes.some(nd=>nd.sopId===id)).length;if(n)out.push({label:'SOPs',n});}
    return out;
  }

  function chainUsers(coll,id){
    const rec=S.get(coll,id); if(!rec)return [];
    return S.active('approvalChains').filter(ch=>(ch.steps||[]).some(s=>coll==='roles'?s.role===rec.key:coll==='people'?s.person===rec.id:s.designation===recKey(rec))).map(ch=>ch.id);
  }
  function activeUsage(coll,id){
    const out=[];
    if((coll==='designations'||coll==='roles'||coll==='people')&&R.approvalChains){const n=chainUsers(coll,id).length;if(n)out.push({label:'Approval chains',n});}
    Object.keys(R).forEach(k=>{const reg=R[k];reg.cols.forEach(c=>{
      if(c.virtual||!c.attr||c.ref!==coll||!(c.type==='ref'||c.type==='multi'))return;
      const n=S.active(reg.coll).filter(x=>{const v=x[c.attr];return Array.isArray(v)?v.includes(id):v===id;}).length;if(n)out.push({label:reg.label,n});
    });});
    return out;
  }
  /* active records using a record, with ids, for "show them" links */
  function activeUsers(coll,id){
    const out=[];
    if((coll==='designations'||coll==='roles'||coll==='people')&&R.approvalChains){const ids=chainUsers(coll,id);if(ids.length)out.push({regKey:'approvalChains',label:'Approval chains',ids});}
    Object.keys(R).forEach(k=>{const reg=R[k];reg.cols.forEach(c=>{
      if(c.virtual||!c.attr||c.ref!==coll||!(c.type==='ref'||c.type==='multi'))return;
      const ids=S.active(reg.coll).filter(x=>{const v=x[c.attr];return Array.isArray(v)?v.includes(id):v===id;}).map(x=>x.id);
      const cur=out.find(o=>o.regKey===k);if(cur)ids.forEach(i=>{if(!cur.ids.includes(i))cur.ids.push(i);});else if(ids.length)out.push({regKey:k,label:reg.label,ids});
    });});
    return out;
  }
  const usageText=u=>u.map(x=>x.n+' '+x.label.toLowerCase()).join(', ');
  /* archive guard: returns a sentence when the record is still used by active records */
  function archiveBlock(coll,id){const u=activeUsage(coll,id);if(coll==='items'){const x=itemUses(id,null,true).filter(y=>y.published);if(x.length)return 'Used in '+x.map(y=>y.label).join(', ');}return u.length?'In use by '+usageText(u):'';}
  function keyOf(reg,v){
    const o=Object.assign({},v);
    reg.cols.forEach(c=>{if(c.type==='enum'&&o[c.k]!=null&&String(o[c.k]).trim()!==''){const x=enumValue(c,o[c.k]);if(x!==undefined)o[c.k]=enumLabel(x);}});
    return reg.key(o);
  }
  const SPECIES_SCOPED=['breeds','stages','tags'];

  /* validation of grid rows. undefined cell = column not provided (keep); '' = clear */
  function isBlankRow(reg,v){return reg.cols.every(c=>!String(v[c.k]==null?'':v[c.k]).trim());}
  function parseDate(raw){
    let m=raw.match(/^(\d{1,2})\/(\d{1,2})\/(\d{4})$/),d,mo,y;
    if(m){d=+m[1];mo=+m[2];y=+m[3];}else{m=raw.match(/^(\d{4})-(\d{2})-(\d{2})$/);if(!m)return null;y=+m[1];mo=+m[2];d=+m[3];}
    const dt=new Date(Date.UTC(y,mo-1,d));
    if(dt.getUTCFullYear()!==y||dt.getUTCMonth()!==mo-1||dt.getUTCDate()!==d)return null;
    return y+'-'+String(mo).padStart(2,'0')+'-'+String(d).padStart(2,'0');
  }
  function validate(regKey,rows,opts){
    const reg=R[regKey]; opts=opts||{}; const create=!!opts.createParents; const pending=opts.pending||{}; const chosen=opts.chosen||{};
    const out=[]; const keyCount={}; const uniq={}; const ids={};
    const keyCol=reg.keyCol||(reg.cols.filter(c=>c.req&&c.type==='text').pop()||reg.cols.find(c=>c.req)||reg.cols[0]).k;
    rows.forEach(r=>{if(!isBlankRow(reg,r.v)){const k=keyOf(reg,r.v);keyCount[k]=(keyCount[k]||0)+1;
      reg.cols.filter(c=>c.unique).forEach(c=>{const x=norm(r.v[c.k]);if(x){uniq[c.k+'|'+x]=(uniq[c.k+'|'+x]||0)+1;}});
      new Set(reg.cols.filter(c=>c.idTag).map(c=>idNorm(r.v[c.k])).filter(Boolean)).forEach(x=>{ids[x]=(ids[x]||0)+1;});}});
    const penLoad={};
    rows.forEach((r,idx)=>{
      const v=r.v; const res={id:r.id,status:'create',issues:{},creates:[],unresolved:[],match:null};
      if(isBlankRow(reg,v)){res.status='empty';out.push(res);return;}
      const RANK={err:3,warn:2,info:1};
      const iss=(k,l,m,extra)=>{const cur=res.issues[k];if(!cur||RANK[l]>RANK[cur[0]])res.issues[k]=[l,m,extra||null];};
      const key=keyOf(reg,v);
      const byId=r.recId?S.get(reg.coll,r.recId):null;
      const byKey=S.all(reg.coll).filter(x=>keyOf(reg,toRow(regKey,x))===key);
      const existing=byId||byKey[0]||null;
      reg.cols.forEach(c=>{
        if(v[c.k]===undefined&&existing)return;
        const raw=String(v[c.k]==null?'':v[c.k]).trim();
        if(!raw){if(c.req&&!(c.reqUnless&&c.reqUnless(v)))iss(c.k,'err','Required');return;}
        if(/^=/.test(raw)){iss(c.k,'err','Formula');return;}
        if(c.type==='num'){if(isNaN(Number(raw)))iss(c.k,'err','Not a number');else if(c.min!=null&&Number(raw)<c.min)iss(c.k,'err','Min '+c.min);}
        if(c.type==='date'&&!parseDate(raw))iss(c.k,'err','YYYY-MM-DD');
        if(c.type==='enum'&&enumValue(c,raw)===undefined)iss(c.k,'err','Use '+c.opts.filter(Boolean).map(enumLabel).join(' · '));
        if(c.type==='steps'){const p=parseSteps(raw);if(p.bad.length)iss(c.k,'err','Unknown: '+p.bad.join(', '));}
        if(c.type==='multienum'){const bad=raw.split(/[;,]/).map(s=>s.trim()).filter(s=>s&&enumValue(c,s)===undefined);if(bad.length)iss(c.k,'err','Unknown: '+bad.join(', '));}
        if(c.unique){const x=norm(raw);if(uniq[c.k+'|'+x]>1)iss(c.k,'err','Duplicate in sheet');const other=S.all(reg.coll).find(y=>norm(y[c.attr])===x&&(!existing||y.id!==existing.id));if(other)iss(c.k,'err','Already used');}
        if(c.idTag){
          const x=idNorm(raw);
          if(/^\d+(\.\d+)?e\+?\d+$/i.test(raw))iss(c.k,'err','Excel turned this tag into a number · format the column as Text');
          if(ids[x]>1)iss(c.k,'err','Tag repeated in sheet');
          const other=S.all(reg.coll).find(y=>(!existing||y.id!==existing.id)&&reg.cols.some(cc=>cc.idTag&&idNorm(y[cc.attr])===x));
          if(other)iss(c.k,'err','Tag already on animal '+(other.rfid||''));
        }
        if(c.type==='ref'||c.type==='path'){
          const f=findRef(regKey,c,raw,v);
          if(f.ambiguous)iss(c.k,'err',f.ambiguous+' matches'+(c.scope?' · set '+R[regKey].cols.find(x=>x.k===c.scope.col).label:''));
          else if(f.rec){
            const lab=c.type==='path'?catPath(f.rec.id):label(c.ref,f.rec);
            if(norm(lab)!==norm(raw)&&norm(fullLabel(c.ref,f.rec))!==norm(raw)&&norm(f.rec.code)!==norm(raw))iss(c.k,'info',raw+' → '+lab);
          }
          else{
            const pend=pending[c.ref]&&pending[c.ref].has(norm(raw));
            const wrong=!pend&&c.scope&&c.scope.attr==='speciesId'?matches(c,S.active(c.ref),norm(raw)).filter(x=>x.speciesId):[];
            if(pend){}
            else if(!nameLike(raw)){iss(c.k,'err','Not a valid '+(R[Object.keys(R).find(k=>R[k].coll===(c.type==='path'?'categories':c.ref))]||{one:c.label}).one.toLowerCase());}
            else if(wrong.length){iss(c.k,'err',raw+' belongs to '+wrong.map(x=>labelById('species',x.speciesId)).join(', ')+', not '+String(v[c.scope.col]||'').trim());}
            else if(f.archived){if(create||chosen[c.k+'|'+norm(raw)]){iss(c.k,'warn','Archived · will restore');res.creates.push(c.label+': '+raw);}else{iss(c.k,'err','Archived · restore or pick another',{create:true,text:raw,archived:true});res.unresolved.push(c.k);}}
            else if(create){iss(c.k,'warn','New');res.creates.push(c.label+': '+raw);}
            else{iss(c.k,'err','Not found',{create:true,text:raw});res.unresolved.push(c.k);}
          }
        }
        if(c.type==='multi'){
          raw.split(/[;,]/).map(s=>s.trim()).filter(Boolean).forEach(t=>{
            const cc=Object.assign({},c,{type:'ref'});
            const f=findRef(regKey,cc,t,v);
            if(f.ambiguous)iss(c.k,'err',t+': '+f.ambiguous+' matches');
            else if(!f.rec){
              const wrong=c.scope&&c.scope.attr==='speciesId'?matches(cc,S.active(c.ref),norm(t)).filter(x=>x.speciesId):[];
              if(wrong.length)iss(c.k,'err',t+' belongs to '+wrong.map(x=>labelById('species',x.speciesId)).join(', '));
              else if(!nameLike(t)){iss(c.k,'err','Not a valid name: '+t);}
              else if(create){iss(c.k,'warn','New: '+t);res.creates.push(c.label+': '+t);}
              else{iss(c.k,'err','Not found: '+t,{create:true,text:t});res.unresolved.push(c.k);}
            }
          });
        }
      });
      if(regKey==='animals'){
        const a=idNorm(v.rfid),b=idNorm(v.tag2);
        if(a&&b&&a===b)iss('tag2','err','Must differ from RFID');
      }
      if(keyCount[key]>1)iss(keyCol,'err','Duplicate in sheet');
      if(byId&&byKey.some(x=>x.id!==byId.id))iss(keyCol,'err','Already exists');
      if(existing){
        res.match=existing;
        const cur=toRow(regKey,existing);
        const same=(c,raw)=>c.bool?(enumValue(c,raw)==='yes')===isOn(existing[c.attr]):c.blankNo?(enumValue(c,raw)||'no')===(existing[c.attr]||'no'):c.type==='enum'?enumValue(c,raw)===existing[c.attr]||norm(raw)===norm(cur[c.k]):norm(raw)===norm(cur[c.k]);
        const changed=reg.cols.some(c=>{if(c.virtual||v[c.k]===undefined)return false;const raw=String(v[c.k]==null?'':v[c.k]).trim();return !same(c,raw);});
        const newStatus=v.status!==undefined?norm(v.status):null;
        res.status=changed||existing.status==='archived'&&newStatus!=='archived'||newStatus&&newStatus!==(existing.status||'active')?'update':'unchanged';
        /* species-scoped values cannot move species or be archived while animals or shed tags use them */
        if(SPECIES_SCOPED.includes(reg.coll)&&v.species!==undefined){
          const sp=findRef(regKey,reg.cols.find(c=>c.k==='species'),v.species,v).rec;
          if((sp?sp.id:'')!==(existing.speciesId||'')){const b=archiveBlock(reg.coll,existing.id);if(b)iss('species','err','Cannot change species · '+b);}
        }
        if(newStatus==='archived'&&existing.status!=='archived'){const b=archiveBlock(reg.coll,existing.id);if(b)iss(keyCol,'err','Cannot archive · '+b);}
      }
      if(reg.check)reg.check(v,iss,existing,opts,rows,idx);
      if(regKey==='animals'){
        const f=findRef('animals',reg.cols.find(c=>c.k==='pen'),v.pen,v);
        if(f.rec&&f.rec.capacity){
          if(penLoad[f.rec.id]==null)penLoad[f.rec.id]=S.active('animals').filter(a=>a.penId===f.rec.id&&!rows.some(rr=>rr.recId===a.id)).length;
          penLoad[f.rec.id]++;
          if(penLoad[f.rec.id]>f.rec.capacity)iss('pen','warn','Over capacity '+penLoad[f.rec.id]+'/'+f.rec.capacity);
        }
        const st=findRef('animals',reg.cols.find(c=>c.k==='stage'),v.stage,v).rec;
        const sxr=findRef('animals',reg.cols.find(c=>c.k==='sex'),v.sex,v).rec;const sx=sxr?norm(sxr.code||sxr.name):norm(v.sex);
        if(st&&st.sex&&st.sex!=='any'&&(sx==='female'||sx==='male')&&sx!==st.sex)iss('stage','warn',st.name+' · '+enumLabel(st.sex)+' only');
      }
      if(regKey==='pens'&&existing&&String(v.capacity==null?'':v.capacity).trim()!==''&&!isNaN(+v.capacity)){const n=S.active('animals').filter(x=>x.penId===existing.id).length;if(+v.capacity<n)iss('capacity','warn','Below '+n+' animals in this pen');}
      if(Object.values(res.issues).some(i=>i[0]==='err'))res.status='error';
      out.push(res);
    });
    return out;
  }

  /* commit validated rows; returns summary */
  function commit(regKey,rows,opts){
    const reg=R[regKey]; opts=opts||{};
    const results=validate(regKey,rows,opts);
    if(opts.strict&&results.some(r=>r.status==='error'))return {created:0,updated:0,unchanged:0,skipped:results.filter(r=>r.status==='error').length,parents:0,blocked:true};
    const sum={created:0,updated:0,unchanged:0,skipped:0,parents:0};
    const before={}; Object.keys(R).forEach(k=>before[R[k].coll]=S.all(R[k].coll).length);
    results.forEach((res,i)=>{
      const v=rows[i].v;
      if(res.status==='empty')return;
      if(res.status==='error'){sum.skipped++;return;}
      if(res.status==='unchanged'){sum.unchanged++;return;}
      if(regKey==='categories'){const r=ensurePath(v.path);sum.created+=r.made?1:0;return;}
      const attrs={};
      reg.cols.forEach(c=>{
        if(c.virtual)return;
        if(v[c.k]===undefined&&res.match)return;
        const raw=String(v[c.k]==null?'':v[c.k]).trim();
        if(!raw){attrs[c.attr]=(c.type==='multi'||c.type==='multienum')?[]:c.bool?false:c.blankNo?'no':'';return;}
        if(c.type==='num')attrs[c.attr]=Number(raw);
        else if(c.type==='enum')attrs[c.attr]=c.bool?enumValue(c,raw)==='yes':enumValue(c,raw);
        else if(c.type==='multienum')attrs[c.attr]=raw.split(/[;,]/).map(s=>s.trim()).filter(Boolean).map(s=>enumValue(c,s));
        else if(c.type==='ref'||c.type==='path'){const f=findRef(regKey,c,raw,v);attrs[c.attr]=f.rec?f.rec.id:createRef(regKey,c,raw,v);}
        else if(c.type==='multi')attrs[c.attr]=raw.split(/[;,]/).map(s=>s.trim()).filter(Boolean).map(t=>{const cc=Object.assign({},c,{type:'ref'});const f=findRef(regKey,cc,t,v);return f.rec?f.rec.id:createRef(regKey,cc,t,v);});
        else if(c.type==='steps')attrs[c.attr]=parseSteps(raw).steps;
        else if(c.type==='date')attrs[c.attr]=parseDate(raw)||raw;
        else if(c.idTag)attrs[c.attr]=raw.replace(/\s+/g,'');
        else attrs[c.attr]=raw;
      });
      if(regKey==='items'){const cid=attrs.categoryId!==undefined?attrs.categoryId:res.match&&res.match.categoryId;if(ownerDept(cid))attrs.depts=withOwner(attrs.depts!==undefined?attrs.depts:res.match&&res.match.depts,cid);}
      if(regKey==='lots'){if(res.match){sum.skipped++;return;}const l=receive(Object.assign({},attrs,{receivedOn:attrs.receivedOn||today()}));sum.created++;return;}
      if(regKey==='settings'&&attrs.value!==undefined&&attrs.value!==''&&!isNaN(Number(attrs.value)))attrs.value=Number(attrs.value);
      if(v.status!==undefined&&reg.statuses.includes(norm(v.status)))attrs.status=norm(v.status);
      if(res.match){if(res.match.status==='archived'&&v.status===undefined)attrs.status='active';Object.assign(res.match,attrs);sum.updated++;}
      else{S.add(reg.coll,attrs);sum.created++;}
    });
    Object.keys(R).forEach(k=>{if(R[k].coll!==reg.coll){const d=S.all(R[k].coll).length-before[R[k].coll];if(d>0)sum.parents+=d;}});
    return sum;
  }

  window.REG={R,KINDS,OWNER_DEPT,ownerDept,withOwner,on:isOn,stockOf,reorderLevel,lowStock,receive,adjust,consume,transfer,writeOff,serialCount,serialAt,serialOverlap,serialMax,isSerialLot,ITEM_REQ,ITEM_SUB_REQ,ITEM_CAPS,itemMissing,catRoot,itemKindOfCat,catSubtree,itemUses,findPath,approversFor,stepOptions,stepLabel,gradeLabel,nameLike,activeUsers,norm,idNorm,enumLabel,enumValue,activeUsage,archiveBlock,usageText,label,fullLabel,labelById,catPath,toRow,cellValue,candidates,findRef,createRef,createChoices,usage,validate,commit,ensurePath,isBlankRow,parseDate};
})();
