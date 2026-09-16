/* Seed data (staging-shaped). buildSeed() returns a fresh state. */
(function(){
  const DEPTS=['Counts','Weighing','Sales','Feed','Preventive Care','Procurement','Health','Milk','People'];
  window.DEPTS=DEPTS;

  function flow(){
    const nodes=[]; let n=0;
    const api={nodes,
      add(type,p){const id='n'+(++n); nodes.push(Object.assign({id,type,label:'',next:[]},p||{})); return id;},
      link(a,b,when){nodes.find(x=>x.id===a).next.push(when?{to:b,when}:{to:b});},
      chain(ids){for(let i=0;i<ids.length-1;i++)api.link(ids[i],ids[i+1]);}
    };
    return api;
  }

  function inspectionFlow(){
    const src=window.INSPECTION_SOURCE, f=flow(), ids=[], byKey={};
    ids.push(f.add('start',{label:'Load arrives for inspection'}));
    const kindMap={text:'text',number:'number',choice:'choice',multi:'multi',vendor:'ref'};
    const addQ=(q,page)=>{
      let id;
      if(q.kind==='media'){
        id=f.add('evidence',{label:q.title,media:q.accepts||['photo','video'],min:q.required?1:0,max:q.max_files||1,page,key:q.id});
      }else{
        const p={label:q.title,answer:kindMap[q.kind]||'text',required:!!q.required,page,key:q.id};
        if(q.options)p.options=q.options.map(o=>({v:o.value,l:o.label}));
        if(q.min!=null)p.min=q.min; if(q.max!=null)p.max=q.max; if(q.unit)p.unit=q.unit;
        if(q.allow_other)p.allowOther=true;
        if(q.kind==='vendor')p.refColl='vendors';
        id=f.add('question',p);
      }
      byKey[q.id]=id; ids.push(id);
      if(q.only_if){const nn=f.nodes.find(x=>x.id===id); nn.onlyIf={q:byKey[q.only_if.question_id],v:q.only_if.value};}
    };
    src.load.forEach(q=>addQ(Object.assign({},q,{id:'load_'+q.id}),'Load details'));
    /* goatos-stg published procurement.animal_purchase v7 = 7 load + 40 animal questions; v7 added Breed and Stage (identity page). */
    const v7=[{id:'b',kind:'text',title:'Breed of animal',required:true},{id:'s',kind:'text',title:'Stage of animal',required:true}];
    src.pages.forEach(p=>{const qs=p.key==='identity'?p.questions.concat(v7.filter(x=>!p.questions.some(y=>y.id===x.id))):p.questions;qs.forEach(q=>addQ(q,p.title));});
    const nd=k=>f.nodes.find(x=>x.id===byKey[k]);
    nd('well_fed').reject={op:'=',value:'no',reason:'Visibly empty or weak'};
    nd('teeth').reject={op:'cannot',value:'',reason:"Mouth can't be opened"};
    const ap=f.add('approval',{label:'Reviewer decision per animal',roleId:'role_ceo',outcomes:['Accept','Reject']});
    const end=f.add('end',{label:'Inspection closed',outcome:'done'});
    ids.push(ap,end); f.chain(ids);
    return f.nodes;
  }

  function linear(steps){
    const f=flow(); const ids=steps.map(s=>f.add(s[0],s[1])); f.chain(ids); return f.nodes;
  }

  function weighingFlow(){
    const f=flow();
    const s=f.add('start',{label:'Open assigned weighing work'});
    const q=f.add('question',{label:'Scan RFID tag',answer:'scan',required:true,page:'Scan'});
    const d=f.add('decision',{label:'Tag already weighed in this bucket?',q:q,op:'duplicate',value:''});
    const blk=f.add('end',{label:'Duplicate scan blocked',outcome:'rejected'});
    const w=f.add('question',{label:'Weight',answer:'number',unit:'kg',min:0,max:20000,required:true,page:'Weight'});
    const v=f.add('evidence',{label:'Weighing video',media:['video'],min:1,max:5,page:'Weight'});
    const a=f.add('approval',{label:'Verifier reviews video and weight',roleId:'role_verifier',outcomes:['Approve','Send back']});
    const e=f.add('end',{label:'Weight accepted',outcome:'done'});
    f.link(s,q); f.link(q,d); f.link(d,blk,'Yes'); f.link(d,w,'No'); f.link(w,v); f.link(v,a); f.link(a,e);
    return f.nodes;
  }

  /* Lump-sum weighing: backed on main by weighing capture.lump_sum {video_min,video_max<=5}
     (admin-web features/sops/weighing-model.ts, backend weighing ErrLumpSumVideoCount). Head count is the
     server census snapshot (docs/decisions/weighing-lump-sum-census-count.md), never typed by the operator. */
  function lumpSumWeighingFlow(){
    const f=flow();
    const s=f.add('start',{label:'Open assigned lump-sum shed'});
    const pen=f.add('question',{label:'Shed / pen',answer:'ref',refColl:'pens',required:true,page:'Shed'});
    const w=f.add('question',{label:'Total weight of the shed',answer:'number',unit:'kg',min:0,max:20000,required:true,page:'Weight'});
    const v=f.add('evidence',{label:'Lump-sum pen video',media:['video'],min:1,max:5,page:'Weight'});
    const a=f.add('approval',{label:'Verifier reviews lump-sum video and total weight',roleId:'role_verifier',outcomes:['Verified','Rework']});
    const ok=f.add('end',{label:'Verified',outcome:'done'});
    const re=f.add('end',{label:'Rework — re-weigh and re-film',outcome:'rejected'});
    f.link(s,pen); f.link(pen,w); f.link(w,v); f.link(v,a); f.link(a,ok,'Verified'); f.link(a,re,'Rework');
    return f.nodes;
  }

  /* Sick-animal report. Mirrors Android feature-health ObservationFormScreen (4 steps: vitals, head, body, final;
     no disease names, no medicines on the operator form). The operator records findings only; the server proposes an
     assessment and the Health Director approves it. Treatment comes from the versioned Health protocol, never from here. */
  function healthFlow(){
    const f=flow();
    const yn=[{v:'no',l:'No'},{v:'yes',l:'Yes'}];
    const q=(label,page,p)=>f.add('question',Object.assign({label,answer:'choice',options:yn,required:true,page},p||{}));
    const s=f.add('start',{label:'Sick animal reported'});
    const ids=[s,
      f.add('question',{label:'RFID or goat tag',answer:'scan',required:true,page:'Animal'}),
      f.add('question',{label:'Rectal temperature',answer:'number',unit:'°F',min:90,max:112,required:true,page:'Vitals'}),
      q('Skin tent','Vitals'),q('Sunken flank','Vitals'),
      f.add('question',{label:'Eyes',answer:'multi',options:[{v:'normal',l:'Normal'},{v:'pale',l:'Pale'},{v:'yellow',l:'Yellow'}],required:true,page:'Vitals'}),
      f.add('question',{label:'Mouth',answer:'multi',options:[{v:'normal',l:'Normal'},{v:'scabs',l:'Scabs'},{v:'froth',l:'Froth'},{v:'cannot_open',l:'Cannot open'}],required:true,page:'Head'}),
      q('Breathing difficulty','Head'),q('Runny nose','Head'),
      q('Left side swollen','Body'),q('Rumen movement absent','Body'),q('Loose motion','Body'),q('Not eating','Body'),
      f.add('question',{label:'Skin & coat',answer:'multi',options:[{v:'normal',l:'Normal'},{v:'ticks',l:'Ticks'},{v:'hair_loss',l:'Hair loss'}],required:true,page:'Body'}),
      q('Wounds','Body'),q('Lumps','Body'),q('Rashes','Body'),
      f.add('question',{label:'Maggots and ear tag',answer:'multi',options:[{v:'normal',l:'Normal'},{v:'body',l:'On the body'},{v:'tag',l:'At the ear tag'},{v:'torn',l:'Ear tag torn'}],required:true,page:'Body'}),
      q('Cannot stand normally','Final'),q('Legs or feet problem','Final'),q('Nervous signs','Final'),
      f.add('evidence',{label:'Animal photo',media:['photo'],min:1,max:3,page:'Final'})];
    f.chain(ids);
    const ap=f.add('approval',{label:'Director approves assessment',roleId:'role_health',outcomes:['Treat in pen','Move to ICU','Check again']});
    const tr=f.add('action',{label:'Treat per Health protocol',link:'#/health/config',linkLabel:'Health protocol'});
    const mv=f.add('child',{label:'Shift to ICU (Health)',sopId:'sop_shift',reason:'health'});
    const e=f.add('end',{label:'Case handed to Health protocol',outcome:'done'});
    f.link(ids[ids.length-1],ap); f.link(ap,tr,'Treat in pen'); f.link(ap,mv,'Move to ICU'); const re=f.add('end',{label:'Re-check requested',outcome:'rejected'}); f.link(ap,re,'Check again'); f.link(mv,tr); f.link(tr,e);
    return f.nodes;
  }

  function destPrepFlow(){
    const f=flow();
    const s=f.add('start',{label:'Transit started'});
    const pen=f.add('question',{label:'Destination pen',answer:'ref',refColl:'pens',required:true,page:'Allocate'});
    const par=f.add('parallel',{label:'Prepare in parallel'});
    const e1=f.add('evidence',{label:'Shed emptied',media:['video'],min:1,max:2,lane:'Shed'});
    const e2=f.add('evidence',{label:'Shed sanitized',media:['video'],min:1,max:2,lane:'Shed'});
    const w1=f.add('action',{label:'Fill water with ORS',itemId:'itm_ors',lane:'Water'});
    const w2=f.add('evidence',{label:'Water trough with ORS',media:['photo'],min:1,max:3,lane:'Water'});
    const j=f.add('join',{label:'Both lanes done'});
    const ap=f.add('approval',{label:'Shed ready',roleId:'role_parkhead',outcomes:['Ready','Redo']});
    const e=f.add('end',{label:'Destination ready',outcome:'done'});
    f.link(s,pen); f.link(pen,par); f.link(par,e1,'Shed'); f.link(e1,e2); f.link(par,w1,'Water'); f.link(w1,w2); f.link(e2,j); f.link(w2,j); f.link(j,ap); f.link(ap,e);
    return f.nodes;
  }

  function transitFlow(){
    const f=flow();
    const s=f.add('start',{label:'Boarding list approved'});
    const t=f.add('question',{label:'Truck',answer:'ref',refColl:'trucks',required:true,page:'Departure'});
    const c=f.add('question',{label:'Animals boarded',answer:'number',unit:'animals',min:0,required:true,page:'Departure'});
    const p=f.add('evidence',{label:'Departure photo',media:['photo'],min:1,max:3,page:'Departure'});
    const r=f.add('repeat',{label:'Transit check',every:3,everyUnit:'hours',everySetting:'set_travel_check',forAmount:3,forUnit:'days',forSetting:'set_travel_days',media:['photo','video']});
    const rc=f.add('question',{label:'Animals received',answer:'number',unit:'animals',min:0,required:true,page:'Arrival'});
    const d=f.add('decision',{label:'Received less than boarded?',q:rc,op:'<',value:'@'+c,page:'Arrival'});
    const ap=f.add('approval',{label:'Shortage review',roleId:'role_procdir',outcomes:['Accept','Investigate']});
    const e=f.add('end',{label:'Arrived',outcome:'done'});
    f.link(s,t); f.link(t,c); f.link(c,p); f.link(p,r); f.link(r,rc); f.link(rc,d); f.link(d,ap,'Yes'); f.link(d,e,'No'); f.link(ap,e);
    return f.nodes;
  }

  function sop(id,dept,title,nodes,v,running,category){
    const at='2026-09-'+String(2+((v*7)%13)).padStart(2,'0')+'T09:30:00Z';
    return {id,dept,title,category:category||'action',nodes,versions:v?[{v,at,by:'Ravi Teja',nodes:JSON.parse(JSON.stringify(nodes)),running:running||0}]:[],status:'active'};
  }

  window.buildSeed=function(){
    const st={meta:{v:3,created:new Date().toISOString()}};
    st.farms=[
      {id:'farm_cbe',code:'CBE',name:'Coimbatore',kind:'core',status:'active'},
      {id:'farm_cpt',code:'CPT',name:'Channapatna',kind:'core',status:'active'}];
    /* stg locations(type=park) CBE / CPT; park codes kept distinct from farm codes */
    st.parks=[
      {id:'park_cbe',code:'CBE-PARK',name:'Coimbatore',farmId:'farm_cbe',status:'active'},
      {id:'park_cpt',code:'CPT-PARK',name:'Channapatna',farmId:'farm_cpt',status:'active'}];
    const L=window.LIFECYCLE_SEED;
    st.stages=L.stages.map((x,i)=>Object.assign({order:i,status:'active'},x));
    st.tags=L.tags.map((x,i)=>Object.assign({id:'tag_'+i,speciesId:'',status:'active'},x));
    st.healthStates=L.healthStates.map((x,i)=>Object.assign({id:'hs_'+i,status:'active'},x));
    const stg=(sp,code)=>(st.stages.find(x=>x.speciesId===sp&&x.code===code)||{}).id||'';

    /* stg active sheds (locations type=shed) with their active shed_partitions. Capacity/stage/sex are not profiled in stg, so left blank. */
    const P=(n,parts)=>[n,parts];
    const seq=(pre,k)=>Array.from({length:k},(_,i)=>pre+(i+1));
    const sheds={
      park_cbe:[P('Castro',seq('',3)),P('Gandhi',seq('',3)),P('Godel 1',seq('Part ',8)),P('Godel 2',seq('Part ',8)),P('Ho Chi Minh',['1']),
        P('Mandela 1',seq('Part ',10)),P('Mandela 2',seq('Part ',8)),P('Sumathi 1',seq('Part ',8)),P('Sumathi 2',seq('Part ',8)),P('Yashoda',seq('',10))],
      park_cpt:[P('Castro',seq('',2)),P('Gandhi',seq('',3)),P('Godel 1',seq('Part ',8)),P('Godel 2',seq('Part ',8)),
        P('Mandela 1',seq('Part ',10)),P('Mandela 2',seq('Part ',10)),P('Old Yashoda',seq('',5)),P('Yashoda',seq('',4))]};
    /* stg shed_profiles: sex blank, has_icu false and lifecycle status null on every row; stage set on these sheds. */
    const stgProfile={'cpt|Gandhi':'NP','cpt|Godel 1':'NP','cpt|Godel 2':'NP'};
    st.pens=[]; st.partitions=[];
    const penId={};
    Object.keys(sheds).forEach(pk=>sheds[pk].forEach(([name,parts])=>{
      const id='pen_'+pk.slice(5)+'_'+name.toLowerCase().replace(/\s+/g,'_'); penId[pk.slice(5)+'|'+name]=id;
      const prof=(stgProfile[pk.slice(5)+'|'+name]||'');
      st.pens.push({id,parkId:pk,name,capacity:'',stageId:prof?stg('sp_goat',prof):'',sex:'',hasIcu:false,lifecycleStatus:'',status:'active'});
      parts.forEach(pt=>st.partitions.push({id:id+'_p'+pt.replace(/\D/g,''),penId:id,name:pt,capacity:'',status:'active'}));
    }));

    st.species=[{id:'sp_goat',name:'Goat',code:'goat',status:'active'},{id:'sp_sheep',name:'Sheep',code:'sheep',status:'active'}];
    /* stg breeds: goat breeds + Anantapur Sheep under sheep (stg also carries a mis-speciesed goat 'Anantapur Sheep' row, not copied). Aliases from stg breed_aliases (non-identical). */
    const goatBreeds=[['Anantapur',[],'review'],['Beetal'],['Beetal x Malai',['Malai x Beetal']],['Beetal x Sojat'],['Boer'],['Boer x Beetal'],['Boer x Malai'],['Boer x Sirohi'],['Boer x Sojat'],
      ['Kenguri',[],'review'],['Malai'],['Malai x Osmanabadi',['Osmanabadi x Malai']],['Malai x Sojat',['Sojat x Malai']],['Osmanabadi'],['Osmanabadi x Sojat',['Sojat x Osmanabadi']],['Sirohi'],['Sojat']];
    const bid=n=>'br_g_'+n.toLowerCase().replace(/[^a-z]+/g,'_');
    st.breeds=goatBreeds.map(([b,al,stt])=>({id:bid(b),speciesId:'sp_goat',name:b,aliases:al||[],status:stt||'active'}));
    st.breeds.push({id:'br_s_anantapur_sheep',speciesId:'sp_sheep',name:'Anantapur Sheep',aliases:[],status:'active'});
    st.sexes=[{id:'sx_gf',speciesId:'sp_goat',name:'Female',status:'active'},{id:'sx_gm',speciesId:'sp_goat',name:'Male',status:'active'},
      {id:'sx_sf',speciesId:'sp_sheep',name:'Female',status:'active'},{id:'sx_sm',speciesId:'sp_sheep',name:'Male',status:'active'}];
    // animals
    const rnd=(()=>{let x=20260917;return()=>{x=(x*1103515245+12345)%2147483648;return x/2147483648;};})();
    st.animals=[];
    const aBreeds=['beetal','sojat','sirohi','malai','osmanabadi','boer'].map(x=>'br_g_'+x);
    const pm=penId['cpt|Mandela 1'], pk=penId['cpt|Godel 1'], pc=penId['cbe|Castro'];
    const partOf=(pen,k)=>(st.partitions.filter(x=>x.penId===pen)[k]||{}).id||'';
    for(let i=0;i<26;i++){
      const pen=i<14?pm:(i<22?pk:pc); const park=pen===pc?'park_cbe':'park_cpt';
      const female=pen===pc?true:(pen===pm?rnd()>.8:rnd()>.45);
      const kid=pen===pk; const stage=stg('sp_goat',kid?'K3':(pen===pc?'FAT-F':(female?'NP':'FAT-M')));
      st.animals.push({id:'ani_'+i,rfid:'982000'+String(Math.floor(rnd()*1e9)).padStart(9,'0'),tag2:'T-'+(1400+Math.floor(rnd()*900)),
        speciesId:'sp_goat',breedId:aBreeds[Math.floor(rnd()*aBreeds.length)],sexId:female?'sx_gf':'sx_gm',stageId:stage,
        dob:'2026-0'+(1+Math.floor(rnd()*6))+'-1'+Math.floor(rnd()*9),weight:+(kid?9+rnd()*6:22+rnd()*18).toFixed(1),
        parkId:park,penId:pen,partitionId:pen===pc?partOf(pen,i%3):partOf(pen,i%4),tagIds:[],status:'active'});
    }
    st.animals.slice(20,24).forEach(a=>{a.speciesId='sp_sheep';a.breedId='br_s_anantapur_sheep';a.sexId=a.sexId==='sx_gf'?'sx_sf':'sx_sm';a.stageId=stg('sp_sheep',a.sexId==='sx_sf'?'FAT-F':'FAT-M');});

    // items
    st.categories=[
      {id:'cat_vac',parentId:'',name:'Vaccines'},{id:'cat_med',parentId:'',name:'Medicines'},{id:'cat_ab',parentId:'cat_med',name:'Antibiotics'},
      {id:'cat_ai',parentId:'cat_med',name:'Anti-inflammatory'},{id:'cat_inj',parentId:'cat_med',name:'Injections'},{id:'cat_dew',parentId:'',name:'Dewormers'},
      {id:'cat_feed',parentId:'',name:'Feed'},{id:'cat_conc',parentId:'cat_feed',name:'Concentrate'},{id:'cat_rough',parentId:'cat_feed',name:'Roughage'},
      {id:'cat_sup',parentId:'',name:'Supplements'},{id:'cat_supplies',parentId:'',name:'Supplies'},{id:'cat_needle',parentId:'cat_supplies',name:'Needles'},
      {id:'cat_elec',parentId:'',name:'Electrical appliances'},{id:'cat_fans',parentId:'cat_elec',name:'Fans'}].map(c=>Object.assign(c,{status:'active'}));
    const it=(id,cat,name,unit,depts,status)=>({id,categoryId:cat,name,unit,depts,status:status||'active'});
    st.items=[
      ...['BT','ET+TT','FMD','Goat Pox','HS','PPR','Sheep Pox'].map((v,i)=>it('itm_vac'+i,'cat_vac',v,'dose',['Preventive Care','Procurement'])),
      it('itm_tylosin','cat_ab','Tylosin','ml',['Health']),it('itm_melox','cat_ai','Meloxicam-Paracetamol','ml',['Health']),
      it('itm_choc','cat_inj','Chocolate injection','ml',['Health'],'review'),it('itm_ors','cat_sup','ORS','g',['Procurement','Health']),
      it('itm_conc','cat_conc','Concentrate mix','kg',['Feed','Procurement']),it('itm_hay','cat_rough','Dry fodder','kg',['Feed']),
      it('itm_needle','cat_needle','Needle 18G','piece',['Health','Preventive Care']),it('itm_fan','cat_fans','Shed fan','piece',['Counts'])];

    st.roles=[['role_ceo','CEO / CXO','cxo'],['role_pcdir','Preventive Care Director','director'],['role_growth','Growth Director','director'],['role_feeddir','Feed Director','director'],
      ['role_health','Health Director','director'],['role_procdir','Procurement Director','director'],['role_procmgr','Procurement Manager','manager'],['role_parkhead','Park Head','manager'],
      ['role_verifier','Verifier',''],['role_operator','Operator',''],['role_breeding','Breeding Director','director'],['role_hr','HR','director']]
      .map(r=>({id:r[0],name:r[1],grade:r[2],status:'active'}));
    st.people=[
      {id:'ppl_ravi',name:'Ravi Teja',roleId:'role_ceo',parkIds:[],phone:'',status:'active'},
      {id:'ppl_op1',name:'Operator 01',roleId:'role_operator',parkIds:['park_cpt'],phone:'',status:'active'},
      {id:'ppl_op2',name:'Operator 02',roleId:'role_operator',parkIds:['park_cbe'],phone:'',status:'active'},
      {id:'ppl_ver1',name:'Verifier 01',roleId:'role_verifier',parkIds:['park_cpt','park_cbe'],phone:'',status:'active'},
      {id:'ppl_ph1',name:'Park Head 01',roleId:'role_parkhead',parkIds:['park_cpt'],phone:'',status:'active'}];
    st.approvers=[
      ['Procurement','Animal selection','role_ceo'],['Procurement','Final boarding list','role_ceo'],['Procurement','Transit shortage','role_procdir'],['Procurement','Warm-up release','role_ceo'],
      ['Weighing','Weight and video','role_verifier'],['Feed','Distribution proof','role_verifier'],['Counts','Birth','role_parkhead'],['Counts','Death','role_parkhead'],['Counts','Shifting','role_parkhead'],
      ['Health','Assessment approval','role_health'],['People','Leave','role_hr']].map((a,i)=>({id:'apr_'+i,dept:a[0],step:a[1],roleId:a[2],status:'active'}));

    /* Read-only mirror of the Procurement vendor register (prod /procurement/vendors) for SOP pickers. */
    st.vendors=[
      {id:'ven_1',name:'Anantapur Livestock Traders',supplies:'Animals',farmId:'farm_cpt',city:'Anantapur',phone:'',status:'active'},
      {id:'ven_2',name:'Sirohi Goat Suppliers',supplies:'Animals',farmId:'farm_cbe',city:'Sirohi',phone:'',status:'active'},
      {id:'ven_3',name:'Kolar Feed Depot',supplies:'Feed',farmId:'farm_cpt',city:'Kolar',phone:'',status:'active'},
      {id:'ven_4',name:'Hosur Transport',supplies:'Transport',farmId:'',city:'Hosur',phone:'',status:'active'}];
    st.trucks=[{id:'trk_1',number:'KA 05 AB 4412',vendorId:'ven_4',capacity:120,status:'active'},{id:'trk_2',number:'TN 37 CK 9021',vendorId:'ven_4',capacity:90,status:'active'}];

    const set=(id,dept,name,value,unit,type)=>({id,dept,name,value,unit,type:type||'number',status:'active'});
    st.settings=[
      set('set_sale_min','Sales','Sale-ready minimum weight',35,'kg'),Object.assign(set('set_sale_tol','Sales','Weight tolerance',200,'g'),{meta:{codeDefault:0,max:1000,source:'weighing/domain/shed_weights.go (default 0 g, max 1000 g); 200 g is the requirement example'}}),
      set('set_report_w','Sales','Reporting weight threshold',30,'kg'),set('set_val_fat','Sales','Valuation rate · fattening',450,'INR/kg'),
      set('set_val_af','Sales','Valuation rate · adult female',600,'INR/kg'),set('set_w_af','Sales','Assumed weight · adult female',40,'kg'),
      set('set_val_am','Sales','Valuation rate · adult male',500,'INR/kg'),set('set_w_am','Sales','Assumed weight · adult male',60,'kg'),
      set('set_val_kid','Sales','Valuation rate · kids',500,'INR/kg'),set('set_w_k0','Sales','Assumed weight · K0/K1',3,'kg'),
      set('set_w_k2','Sales','Assumed weight · K2',8,'kg'),set('set_w_k3','Sales','Assumed weight · K3',15,'kg'),set('set_sale_ahead','Sales','Max sale date ahead',60,'days'),
      set('set_seller_hold','Procurement','Seller holding period',15,'days'),set('set_hold_min','Procurement','Pre-arrival vaccination window · min',28,'days'),
      set('set_hold_max','Procurement','Pre-arrival vaccination window · max',35,'days'),set('set_travel_days','Procurement','Travel duration',3,'days'),
      set('set_travel_check','Procurement','Transit check interval',3,'hours'),set('set_warmup','Procurement','Warm-up period',14,'days'),
      set('set_w_period','Weighing','Default weights period',15,'days'),set('set_w_alert','Weighing','Weighing alert retention',30,'days'),
      set('set_removal','Weighing','Feed & water removal cutoff','21:00','time','time'),
      set('set_lowstock','Feed','Low-stock threshold',5,'days'),set('set_lownotify','Feed','Low-stock notify',7,'days'),set('set_cbe_conc','Feed','CBE concentrate override',55,'kg/day'),
      set('set_vac_cap','Preventive Care','Vaccination daily cap',200,'animals/day'),set('set_vac_buffer','Preventive Care','Capacity buffer',7,'days'),
      set('set_vac_shots','Preventive Care','Max vaccines per visit',3,'shots'),set('set_vac_ops','Preventive Care','Operators per day',3,'people'),
      set('set_health_max','Health','Max course duration',90,'days'),set('set_ble_inactive','Health','Herd signal · inactive after',180,'min'),
      set('set_ble_missing','Health','Herd signal · missing after',30,'min'),
      set('set_milk_sessions','Milk','Max milk feeding sessions',4,'sessions/day'),set('set_colostrum_notify','Milk','Colostrum pre-notify',15,'min')];

    // SOPs
    const tagHold=linear([
      ['start',{label:'Animals approved at seller'}],
      ['question',{label:'Scan RFID of approved animal',answer:'scan',required:true,page:'Tag'}],
      ['question',{label:'Vendor tag matches approved list?',answer:'choice',options:[{v:'yes',l:'Yes'},{v:'no',l:'No'}],required:true,page:'Tag',reject:{op:'=',value:'no',reason:'Not on approved list'}}],
      ['action',{label:'Vaccinate in the Vaccination module (scan per animal, video per plan)',link:'#/vaccination/plan',linkLabel:'Vaccination plan',page:'Vaccinate'}],
      ['question',{label:'Holding start date',answer:'date',required:true,page:'Hold'}],
      ['wait',{label:'Hold at seller',amount:15,unit:'days',setting:'set_seller_hold'}],
      ['end',{label:'Ready for reinspection',outcome:'done'}]]);
    const reinspect=linear([
      ['start',{label:'Holding period over'}],
      ['question',{label:'Scan RFID',answer:'scan',required:true,page:'Reinspect'}],
      ['question',{label:'Is the animal well fed and walking actively?',answer:'choice',options:[{v:'yes',l:'Yes'},{v:'no',l:'No'}],required:true,page:'Reinspect',reject:{op:'=',value:'no',reason:'Visibly empty or weak'}}],
      ['question',{label:'Rectal temperature',answer:'number',unit:'°C',min:30,max:45,required:true,page:'Reinspect'}],
      ['evidence',{label:'Animal photo and video',media:['photo','video'],min:1,max:5,page:'Reinspect'}],
      ['question',{label:'Boarding decision',answer:'choice',options:[{v:'board',l:'Board'},{v:'hold',l:'Hold'},{v:'reject',l:'Reject'}],required:true,page:'Reinspect',reject:{op:'=',value:'reject',reason:'Rejected at reinspection'}}],
      ['approval',{label:'Final boarding list',roleId:'role_ceo',outcomes:['Approve','Send back']}],
      ['evidence',{label:'Truck sanitized',media:['video'],min:1,max:2,page:'Truck'}],
      ['question',{label:'Familiar feed packed',answer:'number',unit:'kg',min:0,required:true,page:'Truck'}],
      ['end',{label:'Ready to depart',outcome:'done'}]]);
    const warmup=linear([
      ['start',{label:'Arrived at destination'}],
      ['question',{label:'Animals received into shed',answer:'number',unit:'animals',min:0,required:true,page:'Arrival'}],
      ['action',{label:'Feed transition per Feed config',link:'#/feed/config',linkLabel:'Feed config',page:'Arrival'}],
      ['repeat',{label:'Daily warm-up check',every:24,everyUnit:'hours',forAmount:14,forUnit:'days',forSetting:'set_warmup',media:['photo']}],
      ['approval',{label:'Release to herd',roleId:'role_ceo',outcomes:['Release','Extend warm-up']}],
      ['end',{label:'Released',outcome:'done'}]]);
    const removal=linear([
      ['start',{label:'Pen scheduled for weighing tomorrow'}],
      ['question',{label:'Pen',answer:'ref',refColl:'pens',required:true,page:'Pen'}],
      ['evidence',{label:'Feed removal video',media:['video'],min:1,max:1,page:'Removal'}],
      ['evidence',{label:'Water removal video',media:['video'],min:1,max:1,page:'Removal'}],
      ['approval',{label:'Verifier reviews removal',roleId:'role_verifier',outcomes:['Approve','Send back']}],
      ['end',{label:'Pen ready for weighing',outcome:'done'}]]);
    const feedDist=linear([
      ['start',{label:'Session due'}],
      ['question',{label:'Pen',answer:'ref',refColl:'pens',required:true,page:'Session'}],
      ['question',{label:'Session',answer:'number',min:1,required:true,page:'Session'}],
      ['evidence',{label:'Feed weight on scale',media:['photo'],min:1,max:1,page:'Proof'}],
      ['evidence',{label:'Feed distribution video',media:['video'],min:1,max:1,page:'Proof'}],
      ['evidence',{label:'Water distribution video',media:['video'],min:1,max:1,page:'Proof'}],
      ['approval',{label:'Verifier reviews proof',roleId:'role_verifier',outcomes:['Approve','Send back']}],
      ['end',{label:'Fed',outcome:'done'}]]);
    const feedPack=linear([
      ['start',{label:'Packing due'}],
      ['question',{label:'Pen',answer:'ref',refColl:'pens',required:true,page:'Bag'}],
      ['question',{label:'Session',answer:'number',min:1,required:true,page:'Bag'}],
      ['evidence',{label:'Packing video per bag',media:['video'],min:1,max:1,page:'Bag'}],
      ['approval',{label:'Verifier reviews packing',roleId:'role_verifier',outcomes:['Approve','Send back']}],
      ['end',{label:'Packed',outcome:'done'}]]);
    const feedTrans=linear([
      ['start',{label:'Bags packed'}],
      ['question',{label:'Pen',answer:'ref',refColl:'pens',required:true,page:'Delivery'}],
      ['evidence',{label:'Transport video',media:['video'],min:1,max:1,page:'Delivery'}],
      ['approval',{label:'Verifier reviews transport',roleId:'role_verifier',outcomes:['Approve','Send back']}],
      ['end',{label:'Delivered',outcome:'done'}]]);

    const shf=flow();
    (function(f){
      const s=f.add('start',{label:'Animals need to move pens'});
      const kind=f.add('question',{label:'Shift type',answer:'choice',options:[{v:'request',l:'Shifting request'},{v:'direction',l:'Shifting direction'}],required:true,page:'Raise'});
      const why=f.add('question',{label:'Reason',answer:'choice',options:[{v:'growth',l:'Growth'},{v:'delivery',l:'Delivery'},{v:'breeding',l:'Breeding'},{v:'health',l:'Health'}],required:true,page:'Raise'});
      const pri=f.add('question',{label:'Priority',answer:'choice',options:[{v:'high',l:'High'},{v:'low',l:'Low'}],required:true,page:'Raise'});
      const from=f.add('question',{label:'From pen',answer:'ref',refColl:'pens',required:true,page:'Raise'});
      const to=f.add('question',{label:'To pen',answer:'ref',refColl:'pens',required:true,page:'Raise'});
      const an=f.add('question',{label:'Scan animals to move',answer:'scan',required:true,page:'Animals'});
      const d1=f.add('decision',{label:'Shifting request?',q:kind,op:'=',value:'request'});
      const ap=f.add('approval',{label:'Park head authorises',roleId:'role_parkhead',outcomes:['Approve','Reject']});
      const d2=f.add('decision',{label:'High priority?',q:pri,op:'=',value:'high'});
      const w=f.add('wait',{label:'Due 9 AM next day (after 1:30 PM: day after)',amount:1,unit:'days'});
      const v=f.add('evidence',{label:'Video at destination pen',media:['video'],min:1,max:1,page:'Complete'});
      const vr=f.add('approval',{label:'Central team verifies video',roleId:'role_verifier',outcomes:['Approve','Send back']});
      const e=f.add('end',{label:'Shift completed',outcome:'done'});
      f.chain([s,kind,why,pri,from,to,an,d1]); f.link(d1,ap,'Yes'); f.link(d1,d2,'No'); f.link(ap,d2); f.link(d2,v,'Yes'); f.link(d2,w,'No'); f.link(w,v); f.link(v,vr); f.link(vr,e);
    })(shf);
    /* Lifecycle SOPs. Sources: Goats and Parks (PARK SHED TAGS, lifecycle 2-4); colostrum session times tasks/domain/templates.go:188-199;
       300 ml bottle feed on K1 entry (drive-docs-findings:418-421); moves use the Pen shifting SOP (Shifting Reports). */
    const k0=linear([
      ['start',{label:'Kid born · K0 - Newborn'}],
      ['question',{label:'Scan kid RFID',answer:'scan',required:true,page:'Kid'}],
      ['action',{label:'1st colostrum feed (right after birth)',page:'Kid'}],
      ['evidence',{label:'1st colostrum video',media:['video'],min:1,max:1,page:'Kid'}],
      /* tasks/domain/templates.go colostrumSessionTimes: 5 IST slots; birth day gets slots not yet started, next day all 5 (up to 10). */
      ['repeat',{label:'Scheduled colostrum feed',times:['07:00','11:00','15:00','18:30','22:00'],every:1,everyUnit:'days',forAmount:2,forUnit:'days',slotCount:10,notifySetting:'set_colostrum_notify',media:['video']}],
      ['child',{label:'Shift to K1 (Growth)',sopId:'sop_shift'}],
      ['end',{label:'Moved to K1',outcome:'done'}]]);
    const k1=linear([
      ['start',{label:'Kid in K1 - Milk Training'}],
      ['action',{label:'Bottle feed 300 ml per session',page:'Milk'}],
      ['evidence',{label:'Milk feeding video',media:['video'],min:1,max:1,page:'Milk'}],
      ['wait',{label:'Max 7 days in K1',amount:7,unit:'days'}],
      ['child',{label:'Shift to K2 (Growth)',sopId:'sop_shift'}],
      ['end',{label:'Moved to K2',outcome:'done'}]]);
    const k2=linear([
      ['start',{label:'Kid in K2 - Milk Drinking'}],
      ['action',{label:'Milk freely from feeding system',page:'Milk'}],
      ['wait',{label:'Until the kid is 78 days old (K3)',until:'age',amount:78,unit:'days of age',stageId:'stg_g_k3'}],
      ['child',{label:'Shift to K3 (Growth)',sopId:'sop_shift'}],
      ['end',{label:'Moved to K3',outcome:'done'}]]);
    const k3=linear([
      ['start',{label:'Kid in K3 - Weaning'}],
      ['action',{label:'Cut milk ration; offer hay, concentrate and water',page:'Weaning'}],
      ['wait',{label:'Until the kid is 85 days old (Fattening)',until:'age',amount:85,unit:'days of age'}],
      ['child',{label:'Shift to Fattening (Growth)',sopId:'sop_shift'}],
      ['end',{label:'Moved to Fattening',outcome:'done'}]]);
    st.sops=[
      sop('sop_inspect','Procurement','Animal purchase inspection',inspectionFlow(),7,6,'event'),
      sop('sop_taghold','Procurement','Tag, vaccinate and hold',tagHold,1,2),
      sop('sop_reinspect','Procurement','Reinspect and board',reinspect,1,1),
      sop('sop_transit','Procurement','Transit',transitFlow(),1,1),
      sop('sop_destprep','Procurement','Destination shed preparation',destPrepFlow(),1,0),
      sop('sop_warmup','Procurement','Warm-up and feed transition',warmup,1,1),
      sop('sop_weigh','Weighing','Individual weighing',weighingFlow(),2,14),
      sop('sop_weigh_lump','Weighing','Lump-sum weighing',lumpSumWeighingFlow(),1,3),
      sop('sop_removal','Weighing','Feed and water removal',removal,1,3),
      sop('sop_feeddist','Feed','Feed distribution',feedDist,3,22,'commodity'),
      sop('sop_feedpack','Feed','Feed packing',feedPack,1,9,'commodity'),
      sop('sop_feedtrans','Feed','Feed transport',feedTrans,1,9,'commodity'),
      Object.assign(sop('sop_fever','Health','Sick animal report',healthFlow(),1,4,'problem'),{source:'Android health observation form · Health protocol'}),
      sop('sop_k0','Milk','K0 colostrum and newborn care',k0,1,0,'event'),
      sop('sop_k1','Milk','K1 milk training',k1,1,0,'action'),
      sop('sop_k2','Milk','K2 milk drinking',k2,1,0,'action'),
      sop('sop_k3','Milk','K3 weaning',k3,1,0,'action'),
      sop('sop_shift','Counts','Pen shifting',shf.nodes,1,5,'action')];

    st.masters=[{id:'mst_proc',dept:'Procurement',title:'Procurement: purchase → transit → warm-up',status:'active',
      versions:[{v:1,at:'2026-09-10T08:00:00Z',by:'Ravi Teja',running:1}],
      stages:[
        {id:'s0',label:'Seller inspection and selection',sopId:'sop_inspect',deps:[],approvalRoleId:'role_ceo',waitDays:'',waitSetting:'',repeatHours:'',repeatSetting:''},
        {id:'s1',label:'Tag, vaccinate and hold',sopId:'sop_taghold',deps:[{stage:'s0',state:'approved'}],approvalRoleId:'',waitDays:15,waitSetting:'set_seller_hold',repeatHours:'',repeatSetting:''},
        {id:'s2',label:'Reinspect and board',sopId:'sop_reinspect',deps:[{stage:'s1',state:'completed'}],approvalRoleId:'role_ceo',waitDays:'',waitSetting:'',repeatHours:'',repeatSetting:''},
        {id:'s3',label:'Transit',sopId:'sop_transit',deps:[{stage:'s2',state:'approved'}],approvalRoleId:'',waitDays:3,waitSetting:'set_travel_days',repeatHours:3,repeatSetting:'set_travel_check'},
        {id:'s4',label:'Prepare destination sheds',sopId:'sop_destprep',deps:[{stage:'s3',state:'started'}],approvalRoleId:'',waitDays:'',waitSetting:'',repeatHours:'',repeatSetting:''},
        {id:'s5',label:'Warm-up and feed transition',sopId:'sop_warmup',deps:[{stage:'s3',state:'completed'},{stage:'s4',state:'completed'}],approvalRoleId:'role_ceo',waitDays:14,waitSetting:'set_warmup',repeatHours:24,repeatSetting:''}],
      cohort:{offered:100,selected:70,boarded:0,received:0}}];
    return st;
  };
})();
