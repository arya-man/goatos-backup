/* Register definitions shared by lists, forms, grids, import and export */
(function(){
  const norm=s=>String(s==null?'':s).trim().toLowerCase().replace(/\s+/g,' ');
  const ST=['active','archived'];
  const ref=(k,label,refc,attr,extra)=>Object.assign({k,label,type:'ref',ref:refc,attr},extra||{});
  const txt=(k,label,extra)=>Object.assign({k,label,type:'text',attr:k},extra||{});
  const num=(k,label,extra)=>Object.assign({k,label,type:'num',attr:k,w:'num'},extra||{});
  const en=(k,label,opts,extra)=>Object.assign({k,label,type:'enum',opts,attr:k},extra||{});
  /* enum values are stored as codes; people see and may type human-case labels */
  const ENUM_WORDS={any:'Any',cxo:'CEO / CXO',ceo_cxo:'CEO / CXO',am:'Assistant manager',assistant_manager:'Assistant manager',yes_no:'Yes / no',multiselect:'Multi-select',growth_cohort:'Growth cohort'};
  const enumLabel=o=>{o=String(o==null?'':o);if(!o)return '';if(ENUM_WORDS[o])return ENUM_WORDS[o];if(o!==o.toLowerCase())return o;const t=o.replace(/_/g,' ');return t[0].toUpperCase()+t.slice(1);};
  const gradeLabel=g=>{g=String(g==null?'':g);return (window.GRADE_LABELS&&window.GRADE_LABELS[g])||enumLabel(g);};
  const enumValue=(c,raw)=>{const n=norm(raw);return c.opts.find(o=>norm(o)===n||norm(enumLabel(o))===n);};
  /* animal identifiers compare ignoring case and spaces */
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
        ref('stage','Stage','stages','stageId'),en('sex','Sex',['mixed','female','male'])],
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
      cols:[ref('species','Species','species','speciesId',{req:1}),txt('name','Sex',{req:1})],
      key:v=>norm(v.species)+'|'+norm(v.name)},
    stages:{label:'Lifecycle stages',one:'Stage',coll:'stages',
      cols:[ref('species','Species','species','speciesId',{req:1}),txt('code','Code',{req:1,w:'num'}),txt('name','Stage',{req:1}),
        num('fromD','From (days)',{min:0}),num('toD','To (days)',{min:0}),en('sex','Sex',['any','female','male'])],
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
        ref('sex','Sex','sexes','sexId',{req:1,scope:{col:'species',attr:'speciesId'}}),ref('stage','Stage','stages','stageId',{scope:{col:'species',attr:'speciesId'}}),
        Object.assign(txt('dob','Date of birth'),{type:'date'}),num('weight','Weight (kg)',{min:0}),
        ref('park','Park','parks','parkId',{req:1}),ref('pen','Pen','pens','penId',{req:1,scope:{col:'park',attr:'parkId'}}),
        ref('partition','Partition','partitions','partitionId',{scope:{col:'pen',attr:'penId'}}),
        Object.assign(ref('tags','Tags','tags','tagIds',{scope:{col:'species',attr:'speciesId',allowBlank:true}}),{type:'multi'})],
      key:v=>idNorm(v.rfid),keyCol:'rfid'},
    categories:{label:'Categories',one:'Category',coll:'categories',
      cols:[{k:'path',label:'Category path',type:'path',req:1,w:'wide'}],key:v=>norm(v.path)},
    items:{label:'Items',one:'Item',coll:'items',icon:'folder',
      cols:[txt('name','Item',{req:1,w:'wide'}),{k:'category',label:'Category',type:'path',attr:'categoryId',w:'wide'},txt('unit','Unit',{w:'num'}),
        Object.assign(en('depts','Departments',DEPTS),{type:'multienum'})],
      key:v=>norm(v.name)},
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
      key:v=>norm(v.dept)+'|'+norm(v.name)}
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
    S.load=function(){const r=load.apply(S,arguments);migrate(S.state);S.save();return S.state||r;};
    S.reset=function(){const r=reset.apply(S,arguments);migrate(S.state);S.save();return r;};}

  /* optional registers exist only while their seed collection does */
  const OPTIONAL=[];
  try{const st=window.buildSeed?window.buildSeed():null;if(st&&window.seedRefs)window.seedRefs(st);
    if(st)OPTIONAL.forEach(k=>{if(!Array.isArray(st[R[k].coll]))delete R[k];});}catch(e){}
  Object.keys(R).forEach(k=>{R[k].key_=k; if(!R[k].statuses)R[k].statuses=ST;});

  /* labels */
  function catPath(id){const out=[];let c=S.get('categories',id),g=0;while(c&&g++<20){out.unshift(c.name);c=S.get('categories',c.parentId);}return out.join(' > ');}
  function label(coll,rec){
    if(!rec)return '';
    if(coll==='categories')return catPath(rec.id);
    if(coll==='partitions'){const p=S.get('pens',rec.penId);return rec.name;}
    return rec.name||rec.number||rec.code||rec.rfid||'';
  }
  function fullLabel(coll,rec){
    if(!rec)return '';
    if(coll==='partitions'){const p=S.get('pens',rec.penId);return (p?p.name+' - ':'')+rec.name;}
    if(coll==='pens'){const p=S.get('parks',rec.parkId);return rec.name+(p?' · '+p.name:'');}
    if(coll==='breeds'||coll==='stages'||coll==='sexes'){const s=S.get('species',rec.speciesId);return label(coll,rec)+(s?' · '+s.name:'');}
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
    text=String(text||'').trim(); if(!text||col.type==='enum'||col.type==='multienum'||!nameLike(text))return [];
    const refCol=Object.assign({},col,{type:col.type==='path'?'path':'ref'});
    if(col.type==='path')return [{label:'Create “'+text+'”',run:()=>createRef(regKey,refCol,text,row)}];
    if(!col.scope&&(col.ref==='stages'||col.ref==='breeds'||col.ref==='sexes'))return S.active('species').map(sp=>({label:'Create “'+text+'” · '+sp.name,run:()=>{const id=createRef(regKey,refCol,text,row);const r=S.get(col.ref,id);if(r)r.speciesId=sp.id;return id;}}));
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
    if(coll==='roles'||coll==='items'||coll==='settings'){const n=S.all('sops').filter(s=>s.nodes.some(nd=>nd.roleId===id||nd.itemId===id||nd.setting===id||nd.everySetting===id||nd.forSetting===id)).length;if(n)out.push({label:'SOPs',n});}
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
  function archiveBlock(coll,id){const u=activeUsage(coll,id);return u.length?'In use by '+usageText(u):'';}
  function keyOf(reg,v){
    const o=Object.assign({},v);
    reg.cols.forEach(c=>{if(c.type==='enum'&&o[c.k]!=null&&String(o[c.k]).trim()!==''){const x=enumValue(c,o[c.k]);if(x!==undefined)o[c.k]=enumLabel(x);}});
    return reg.key(o);
  }
  const SPECIES_SCOPED=['breeds','stages','sexes','tags'];

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
        if(!raw){if(c.req)iss(c.k,'err','Required');return;}
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
        const same=(c,raw)=>c.type==='enum'?enumValue(c,raw)===existing[c.attr]||norm(raw)===norm(cur[c.k]):norm(raw)===norm(cur[c.k]);
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
      if(reg.check)reg.check(v,iss,existing);
      if(regKey==='animals'){
        const f=findRef('animals',reg.cols.find(c=>c.k==='pen'),v.pen,v);
        if(f.rec&&f.rec.capacity){
          if(penLoad[f.rec.id]==null)penLoad[f.rec.id]=S.active('animals').filter(a=>a.penId===f.rec.id&&!rows.some(rr=>rr.recId===a.id)).length;
          penLoad[f.rec.id]++;
          if(penLoad[f.rec.id]>f.rec.capacity)iss('pen','warn','Over capacity '+penLoad[f.rec.id]+'/'+f.rec.capacity);
        }
        const st=findRef('animals',reg.cols.find(c=>c.k==='stage'),v.stage,v).rec;
        const sx=norm(v.sex);
        if(st&&st.sex&&st.sex!=='any'&&(sx==='female'||sx==='male')&&sx!==st.sex)iss('stage','warn',st.name+' · '+enumLabel(st.sex)+' only');
      }
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
        if(!raw){attrs[c.attr]=(c.type==='multi'||c.type==='multienum')?[]:'';return;}
        if(c.type==='num')attrs[c.attr]=Number(raw);
        else if(c.type==='enum')attrs[c.attr]=enumValue(c,raw);
        else if(c.type==='multienum')attrs[c.attr]=raw.split(/[;,]/).map(s=>s.trim()).filter(Boolean).map(s=>enumValue(c,s));
        else if(c.type==='ref'||c.type==='path'){const f=findRef(regKey,c,raw,v);attrs[c.attr]=f.rec?f.rec.id:createRef(regKey,c,raw,v);}
        else if(c.type==='multi')attrs[c.attr]=raw.split(/[;,]/).map(s=>s.trim()).filter(Boolean).map(t=>{const cc=Object.assign({},c,{type:'ref'});const f=findRef(regKey,cc,t,v);return f.rec?f.rec.id:createRef(regKey,cc,t,v);});
        else if(c.type==='steps')attrs[c.attr]=parseSteps(raw).steps;
        else if(c.type==='date')attrs[c.attr]=parseDate(raw)||raw;
        else if(c.idTag)attrs[c.attr]=raw.replace(/\s+/g,'');
        else attrs[c.attr]=raw;
      });
      if(regKey==='settings'&&attrs.value!==undefined&&attrs.value!==''&&!isNaN(Number(attrs.value)))attrs.value=Number(attrs.value);
      if(v.status!==undefined&&reg.statuses.includes(norm(v.status)))attrs.status=norm(v.status);
      if(res.match){if(res.match.status==='archived'&&v.status===undefined)attrs.status='active';Object.assign(res.match,attrs);sum.updated++;}
      else{S.add(reg.coll,attrs);sum.created++;}
    });
    Object.keys(R).forEach(k=>{if(R[k].coll!==reg.coll){const d=S.all(R[k].coll).length-before[R[k].coll];if(d>0)sum.parents+=d;}});
    return sum;
  }

  window.REG={R,approversFor,stepOptions,stepLabel,gradeLabel,nameLike,activeUsers,norm,idNorm,enumLabel,enumValue,activeUsage,archiveBlock,usageText,label,fullLabel,labelById,catPath,toRow,cellValue,candidates,findRef,createRef,createChoices,usage,validate,commit,ensurePath,isBlankRow,parseDate};
})();
