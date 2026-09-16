/* Register definitions shared by lists, forms, grids, import and export */
(function(){
  const norm=s=>String(s==null?'':s).trim().toLowerCase().replace(/\s+/g,' ');
  const ST=['active','archived'];
  const ref=(k,label,refc,attr,extra)=>Object.assign({k,label,type:'ref',ref:refc,attr},extra||{});
  const txt=(k,label,extra)=>Object.assign({k,label,type:'text',attr:k},extra||{});
  const num=(k,label,extra)=>Object.assign({k,label,type:'num',attr:k,w:'num'},extra||{});
  const en=(k,label,opts,extra)=>Object.assign({k,label,type:'enum',opts,attr:k},extra||{});

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
      statuses:['active','review','archived'],key:v=>norm(v.species)+'|'+norm(v.name)},
    sexes:{label:'Sexes',one:'Sex',coll:'sexes',
      cols:[ref('species','Species','species','speciesId',{req:1}),txt('name','Sex',{req:1})],
      key:v=>norm(v.species)+'|'+norm(v.name)},
    stages:{label:'Lifecycle stages',one:'Stage',coll:'stages',
      cols:[ref('species','Species','species','speciesId',{req:1}),txt('code','Code',{req:1,w:'num'}),txt('name','Stage',{req:1}),
        num('fromD','From (days)',{min:0}),num('toD','To (days)',{min:0}),en('sex','Sex',['any','female','male'])],
      key:v=>norm(v.species)+'|'+norm(v.code),
      check:(v,iss)=>{if(v.fromD!==''&&v.toD!==''&&v.fromD!=null&&v.toD!=null&&!isNaN(v.fromD)&&!isNaN(v.toD)&&Number(v.toD)<Number(v.fromD))iss('toD','err','Before From');}},
    tags:{label:'Tags & groups',one:'Tag',coll:'tags',
      cols:[ref('species','Species','species','speciesId',{blankLabel:'All species'}),txt('name','Name',{req:1}),en('kind','Kind',['tag','group'])],
      key:v=>norm(v.species)+'|'+norm(v.name)+'|'+norm(v.kind||'tag')},
    healthStates:{label:'Health states',one:'Health state',coll:'healthStates',
      cols:[txt('name','Name',{req:1,w:'wide'}),en('group','Group',['Milk Kids','Fattening Kids','Adults']),num('fromD','From (days)'),num('toD','To (days)')],key:v=>norm(v.name)},
    animals:{label:'Animals',one:'Animal',coll:'animals',icon:'list-checks',
      cols:[txt('rfid','RFID',{req:1,w:'wide',unique:1}),txt('tag2','Second tag',{unique:1}),
        ref('species','Species','species','speciesId',{req:1}),ref('breed','Breed','breeds','breedId',{scope:{col:'species',attr:'speciesId'}}),
        ref('sex','Sex','sexes','sexId',{req:1,scope:{col:'species',attr:'speciesId'}}),ref('stage','Stage','stages','stageId',{scope:{col:'species',attr:'speciesId'}}),
        Object.assign(txt('dob','Date of birth'),{type:'date'}),num('weight','Weight (kg)',{min:0}),
        ref('park','Park','parks','parkId'),ref('pen','Pen','pens','penId',{scope:{col:'park',attr:'parkId'}}),
        ref('partition','Partition','partitions','partitionId',{scope:{col:'pen',attr:'penId'}}),
        Object.assign(ref('tags','Tags','tags','tagIds',{scope:{col:'species',attr:'speciesId',allowBlank:true}}),{type:'multi'})],
      key:v=>norm(v.rfid),keyCol:'rfid'},
    categories:{label:'Categories',one:'Category',coll:'categories',
      cols:[{k:'path',label:'Category path',type:'path',req:1,w:'wide'}],key:v=>norm(v.path)},
    items:{label:'Items',one:'Item',coll:'items',icon:'folder',
      cols:[txt('name','Item',{req:1,w:'wide'}),{k:'category',label:'Category',type:'path',attr:'categoryId',w:'wide'},txt('unit','Unit',{w:'num'}),
        Object.assign(en('depts','Departments',DEPTS),{type:'multienum'})],
      statuses:['active','review','archived'],key:v=>norm(v.name)},
    roles:{label:'Roles',one:'Role',coll:'roles',
      cols:[txt('name','Role',{req:1,w:'wide'}),en('grade','Grade',['','cxo','director','manager','assistant_manager'])],key:v=>norm(v.name)},
    people:{label:'People',one:'Person',coll:'people',icon:'user',
      cols:[txt('name','Name',{req:1,w:'wide'}),ref('role','Role','roles','roleId',{req:1}),Object.assign(ref('parks','Parks','parks','parkIds'),{type:'multi',blankLabel:'All parks'}),txt('email','Email',{unique:1}),txt('phone','Phone')],
      key:v=>norm(v.name)},
    approvers:{label:'Approvers',one:'Approver',coll:'approvers',
      cols:[en('dept','Department',DEPTS,{req:1}),txt('step','Approval step',{req:1,w:'wide'}),ref('role','Approver role','roles','roleId',{req:1})],
      key:v=>norm(v.dept)+'|'+norm(v.step)},
    vendors:{label:'Vendors',one:'Vendor',coll:'vendors',icon:'truck',
      cols:[txt('name','Vendor',{req:1,w:'wide'}),en('supplies','Supplies',['Animals','Feed','Transport','Medicines','Other']),ref('farm','Farm','farms','farmId'),txt('city','City'),txt('phone','Phone')],
      key:v=>norm(v.name)},
    trucks:{label:'Trucks',one:'Truck',coll:'trucks',icon:'truck',
      cols:[txt('number','Vehicle number',{req:1,w:'wide',unique:1}),ref('vendor','Vendor','vendors','vendorId'),num('capacity','Capacity (animals)',{min:0})],
      key:v=>norm(v.number)},
    statusDefs:{label:'Status definitions',one:'Status',coll:'statusDefs',
      cols:[en('axis','Axis',['lifecycle','reproductive','health','growth_cohort','management'],{req:1}),txt('code','Code',{req:1,w:'num'}),txt('name','Status',{req:1}),num('days','Expected days',{min:0})],
      key:v=>norm(v.axis)+'|'+norm(v.code)},
    exitReasons:{label:'Exit reasons',one:'Exit reason',coll:'exitReasons',cols:[txt('name','Exit reason',{req:1,w:'wide'})],key:v=>norm(v.name)},
    purposes:{label:'Animal purposes',one:'Purpose',coll:'purposes',cols:[txt('name','Purpose',{req:1,w:'wide'})],key:v=>norm(v.name)},
    movementReasons:{label:'Movement reasons',one:'Movement reason',coll:'movementReasons',cols:[txt('name','Reason',{req:1,w:'wide'})],key:v=>norm(v.name)},
    weightBands:{label:'Weight bands',one:'Weight band',coll:'weightBands',
      cols:[ref('species','Species','species','speciesId',{blankLabel:'All species'}),txt('name','Band',{req:1}),num('fromKg','From (kg)',{min:0}),num('toKg','To (kg)',{min:0})],
      key:v=>norm(v.species)+'|'+norm(v.name)},
    rationGroups:{label:'Ration groups',one:'Ration group',coll:'rationGroups',
      cols:[txt('name','Ration group',{req:1,w:'wide'}),Object.assign(ref('breeds','Breeds','breeds','breedIds'),{type:'multi'})],key:v=>norm(v.name)},
    diseases:{label:'Diseases',one:'Disease',coll:'diseases',cols:[txt('name','Disease',{req:1,w:'wide'}),en('ageBand','Age band',['','adult','kid'])],key:v=>norm(v.name)+'|'+norm(v.ageBand)},
    symptoms:{label:'Symptoms',one:'Symptom',coll:'symptoms',cols:[txt('code','Code',{req:1,w:'num'}),txt('name','Symptom',{req:1,w:'wide'})],key:v=>norm(v.code)},
    deathCauses:{label:'Death causes',one:'Death cause',coll:'deathCauses',cols:[txt('name','Death cause',{req:1,w:'wide'}),ref('disease','Disease','diseases','diseaseId')],key:v=>norm(v.name)},
    marketCities:{label:'Market cities',one:'Market city',coll:'marketCities',cols:[txt('name','City',{req:1,w:'wide'}),txt('state','State')],key:v=>norm(v.name)},
    sopCategories:{label:'SOP categories',one:'SOP category',coll:'sopCategories',cols:[txt('code','Key',{req:1,w:'num'}),txt('name','Category',{req:1,w:'wide'})],key:v=>norm(v.code)},
    taskTypes:{label:'Task types',one:'Task type',coll:'taskTypes',cols:[txt('code','Key',{req:1,w:'wide'}),txt('name','Task type',{req:1,w:'wide'}),en('answer','Answer',['none','yes_no','select','multiselect','number','text'])],key:v=>norm(v.code)},
    settings:{label:'Business settings',one:'Setting',coll:'settings',
      cols:[en('dept','Department',DEPTS,{req:1}),txt('name','Setting',{req:1,w:'wide'}),txt('value','Value',{req:1,w:'num'}),txt('unit','Unit',{w:'num'})],
      key:v=>norm(v.dept)+'|'+norm(v.name)}
  };
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
    if(col.type==='multienum')return (v||[]).join('; ');
    if(col.type==='date'&&v&&/^\d{4}-\d{2}-\d{2}$/.test(v))return v.slice(8,10)+'/'+v.slice(5,7)+'/'+v.slice(0,4);
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
    let hits=list.filter(x=>norm(label(col.ref,x))===n||(col.ref!=='parks'&&norm(x.code)===n)||norm(x.number)===n);
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
    if(c==='trucks')attrs.number=text;
    else if(c==='stages'){attrs.code=text.replace(/\s+/g,'').toUpperCase().slice(0,10);attrs.name=text;attrs.sex='any';attrs.fromD='';attrs.toD='';}
    else if(c==='farms'){attrs.code=text.replace(/\s+/g,'').toUpperCase().slice(0,4);attrs.name=text;}
    else if(c==='parks'){attrs.name=text;attrs.code='';}
    else if(c==='tags'){attrs.name=text;attrs.kind='tag';attrs.speciesId=attrs.speciesId||'';}
    else if(c==='roles'){attrs.name=text;attrs.grade='';}
    else if(c==='vendors'){attrs.name=text;attrs.supplies='Other';}
    else attrs.name=text;
    return S.add(c,attrs).id;
  }
  /* inline-create choices for a picker: one per possible parent when the parent is not chosen */
  function createChoices(regKey,col,row,text){
    text=String(text||'').trim(); if(!text||col.type==='enum'||col.type==='multienum')return [];
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
    if(coll==='sops'){const n=S.all('masters').filter(m=>m.stages.some(s=>s.sopId===id)).length+S.all('sops').filter(s=>s.nodes.some(nd=>nd.sopId===id)).length;if(n)out.push({label:'SOPs',n});}
    return out;
  }

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
    const reg=R[regKey]; opts=opts||{}; const create=opts.createParents!==false; const pending=opts.pending||{};
    const out=[]; const keyCount={}; const uniq={};
    const keyCol=reg.keyCol||(reg.cols.filter(c=>c.req&&c.type==='text').pop()||reg.cols.find(c=>c.req)||reg.cols[0]).k;
    rows.forEach(r=>{if(!isBlankRow(reg,r.v)){const k=reg.key(r.v);keyCount[k]=(keyCount[k]||0)+1;reg.cols.filter(c=>c.unique).forEach(c=>{const x=norm(r.v[c.k]);if(x){uniq[c.k+'|'+x]=(uniq[c.k+'|'+x]||0)+1;}});}});
    const penLoad={};
    rows.forEach((r,idx)=>{
      const v=r.v; const res={id:r.id,status:'create',issues:{},creates:[],match:null};
      if(isBlankRow(reg,v)){res.status='empty';out.push(res);return;}
      const iss=(k,l,m)=>{if(!res.issues[k]||res.issues[k][0]!=='err')res.issues[k]=[l,m];};
      const key=reg.key(v);
      const byId=r.recId?S.get(reg.coll,r.recId):null;
      const byKey=S.all(reg.coll).filter(x=>reg.key(toRow(regKey,x))===key);
      const existing=byId||byKey[0]||null;
      reg.cols.forEach(c=>{
        if(v[c.k]===undefined&&existing)return;
        const raw=String(v[c.k]==null?'':v[c.k]).trim();
        if(!raw){if(c.req)iss(c.k,'err','Required');return;}
        if(c.type==='num'){if(isNaN(Number(raw)))iss(c.k,'err','Not a number');else if(c.min!=null&&Number(raw)<c.min)iss(c.k,'err','Min '+c.min);}
        if(c.type==='date'&&!parseDate(raw))iss(c.k,'err','DD/MM/YYYY');
        if(c.type==='enum'&&!c.opts.some(o=>norm(o)===norm(raw)))iss(c.k,'err',c.opts.filter(Boolean).join(' · '));
        if(c.type==='multienum'){const bad=raw.split(/[;,]/).map(s=>s.trim()).filter(s=>s&&!c.opts.some(o=>norm(o)===norm(s)));if(bad.length)iss(c.k,'err','Unknown: '+bad.join(', '));}
        if(c.unique){const x=norm(raw);if(uniq[c.k+'|'+x]>1)iss(c.k,'err','Duplicate in sheet');const other=S.all(reg.coll).find(y=>norm(y[c.attr])===x&&(!existing||y.id!==existing.id));if(other)iss(c.k,'err','Already used');}
        if(c.type==='ref'||c.type==='path'){
          const f=findRef(regKey,c,raw,v);
          if(f.ambiguous)iss(c.k,'err',f.ambiguous+' matches'+(c.scope?' · set '+R[regKey].cols.find(x=>x.k===c.scope.col).label:''));
          else if(!f.rec){
            const pend=pending[c.ref]&&pending[c.ref].has(norm(raw));
            if(pend){}
            else if(f.archived){if(create){iss(c.k,'warn','Archived · will restore');res.creates.push(c.label+': '+raw);}else iss(c.k,'err','Archived');}
            else if(create){iss(c.k,'warn','New');res.creates.push(c.label+': '+raw);}
            else iss(c.k,'err','Not found');
          }
        }
        if(c.type==='multi'){
          raw.split(/[;,]/).map(s=>s.trim()).filter(Boolean).forEach(t=>{
            const f=findRef(regKey,Object.assign({},c,{type:'ref'}),t,v);
            if(f.ambiguous)iss(c.k,'err',t+': '+f.ambiguous+' matches');
            else if(!f.rec){if(create){iss(c.k,'warn','New: '+t);res.creates.push(c.label+': '+t);}else iss(c.k,'err','Not found: '+t);}
          });
        }
      });
      if(keyCount[key]>1)iss(keyCol,'err','Duplicate in sheet');
      if(byId&&byKey.some(x=>x.id!==byId.id))iss(keyCol,'err','Already exists');
      if(existing){
        res.match=existing;
        const cur=toRow(regKey,existing);
        const changed=reg.cols.some(c=>{if(c.virtual||v[c.k]===undefined)return false;const raw=String(v[c.k]==null?'':v[c.k]).trim();return norm(raw)!==norm(cur[c.k]);});
        res.status=changed||existing.status==='archived'?'update':'unchanged';
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
        if(st&&st.sex&&st.sex!=='any'&&(sx==='female'||sx==='male')&&sx!==st.sex)iss('stage','warn',st.name+' · '+st.sex+' only');
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
        else if(c.type==='enum')attrs[c.attr]=c.opts.find(o=>norm(o)===norm(raw));
        else if(c.type==='multienum')attrs[c.attr]=raw.split(/[;,]/).map(s=>s.trim()).filter(Boolean).map(s=>c.opts.find(o=>norm(o)===norm(s)));
        else if(c.type==='ref'||c.type==='path'){const f=findRef(regKey,c,raw,v);attrs[c.attr]=f.rec?f.rec.id:createRef(regKey,c,raw,v);}
        else if(c.type==='multi')attrs[c.attr]=raw.split(/[;,]/).map(s=>s.trim()).filter(Boolean).map(t=>{const cc=Object.assign({},c,{type:'ref'});const f=findRef(regKey,cc,t,v);return f.rec?f.rec.id:createRef(regKey,cc,t,v);});
        else if(c.type==='date')attrs[c.attr]=parseDate(raw)||raw;
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

  window.REG={R,norm,label,fullLabel,labelById,catPath,toRow,cellValue,candidates,findRef,createRef,createChoices,usage,validate,commit,ensurePath,isBlankRow,parseDate};
})();
