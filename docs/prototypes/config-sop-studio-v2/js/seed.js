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
      if(q.hint){f.nodes.find(x=>x.id===id).hint=q.hint;}
      if(q.only_if){const nn=f.nodes.find(x=>x.id===id); nn.onlyIf={q:byKey[q.only_if.question_id],v:q.only_if.value};}
    };
    /* goatos-stg published procurement.animal_purchase v7: 7 load + 40 animal questions, stg keys and operator hints kept.
       Load keys keep the stg key (w, h, ...); only a key that also exists on an animal page (notes) gets a load_ prefix. */
    const pageKeys=new Set(src.pages.flatMap(p=>p.questions.map(q=>q.id)));
    src.load.forEach(q=>addQ(Object.assign({},q,{id:pageKeys.has(q.id)?'load_'+q.id:q.id}),'Load details'));
    src.pages.forEach(p=>p.questions.forEach(q=>addQ(q,p.title)));
    const nd=k=>f.nodes.find(x=>x.id===byKey[k]);
    nd('well_fed').reject={op:'=',value:'no',reason:'Visibly empty or weak'};
    nd('teeth').reject={op:'cannot',value:'',reason:"Mouth can't be opened"};
    const ap=f.add('approval',{label:'CEO decides per animal on the web',roleId:'role_ceo_internal',outcomes:['Accept','Reject']});
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
    const s=f.add('start',{label:'Open assigned lump-sum pen'});
    const pen=f.add('question',{label:'Pen',answer:'ref',refColl:'pens',required:true,page:'Pen'});
    const w=f.add('question',{label:'Total weight of the pen',answer:'number',unit:'kg',min:0,max:20000,required:true,page:'Weight'});
    const v=f.add('evidence',{label:'Lump-sum pen video',media:['video'],min:1,max:5,page:'Weight'});
    const a=f.add('approval',{label:'Verifier reviews lump-sum video and total weight',roleId:'role_verifier',outcomes:['Verified','Rework']});
    const ok=f.add('end',{label:'Verified',outcome:'done'});
    const re=f.add('end',{label:'Rework — re-weigh and re-film',outcome:'rejected'});
    f.link(s,pen); f.link(pen,w); f.link(w,v); f.link(v,a); f.link(a,ok,'Verified'); f.link(a,re,'Rework');
    return f.nodes;
  }

  /* Sick-animal report = Android feature-health ObservationFormScreen.kt + ObservationForm.kt (ObservationOptions), field for field:
     4 steps Vitals / Head / Body / Final; sex- and kid-specific cards on Final. The animal's sex and kid class come from its record,
     so those pages are named "Female only", "Male only", "Kid only", "Milk kid only". No photo step, no disease names, no medicines.
     Director decision = DiagnosisProposalScreen.kt buttons "Approve treatment" / "Treat none of these"; treatment follows Health Config. */
  function healthFlow(){
    const f=flow();
    const o=(...xs)=>xs.map(([v,l])=>({v,l}));
    const yn=o(['no','No'],['yes','Yes']);
    const Q=(label,page,answer,options,p)=>f.add('question',Object.assign({label,answer,required:true,page},options?{options}:{},p||{}));
    const s=f.add('start',{label:'Sick animal reported'});
    const ids=[s,
      Q('Animal RFID','Animal','scan'),
      Q('Rectal temperature','Vitals','number',null,{unit:'°F',hint:'Below 100 warm it now · above 106 cool it now'}),
      Q('FAMACHA','Vitals','choice',o(['1','1'],['2','2'],['3','3'],['4','4'],['5','5'])),
      Q('FAMACHA · Yellow','Vitals','choice',yn,{hint:'Same look at the eyelid as the FAMACHA score'}),
      Q('Skin tent','Vitals','choice',o(['lt2','Snaps back'],['2-4','Slow'],['gt4','Very slow'])),
      Q('Sunken flank','Vitals','choice',yn),
      Q('Eyes','Head','multi',o(['normal','Normal'],['red','Red'],['cloudy','Cloudy'],['discharge','Discharge'])),
      Q('Mouth','Head','multi',o(['normal','Normal'],['scabs','Scabs'],['froth','Froth'],['cannot_open','Cannot open'])),
      Q('Breathing','Head','multi',o(['normal','Normal'],['fast','Fast'],['labored','Struggling'],['cough','Coughing'],['pant','Panting'])),
      Q('Runny nose','Head','choice',yn),
      Q('Left side','Body · gut and belly','multi',o(['normal','Normal'],['bloating','Blown up'],['acidosis','Water sound'])),
      Q('Rumen movement','Body · gut and belly','choice',o(['felt','Moving'],['not_felt','Not moving'])),
      Q('Loose motion','Body · gut and belly','choice',yn),
      Q('Eating','Body · gut and belly','multi',o(['normal','Eating well'],['not_eating','Not eating'],['concentrate','Took feed'],['green_feed','Took green'],['dry_feed','Took dry'])),
      Q('Skin & coat','Body · skin, body, legs','multi',o(['normal','Normal'],['ticks','Ticks'],['hair_loss','Hair loss'])),
      Q('Wounds','Body · skin, body, legs','multi',o(['no','None'],['horn','Horn'],['neck','Neck'],['body','Body'],['legs','Legs'])),
      Q('Lumps','Body · skin, body, legs','choice',o(['no','None'],['neck','Neck'],['body','Body'])),
      Q('Rashes','Body · skin, body, legs','choice',o(['none','None'],['flat_itchy','Flat and itchy'],['nodular','Raised bumps'])),
      Q('Maggots and ear tag','Body · skin, body, legs','multi',o(['normal','Normal'],['body','On the body'],['tag','At the ear tag'],['torn','Ear tag torn'])),
      Q('How it stands','Body · skin, body, legs','choice',o(['standing','Standing'],['down','Cannot stand'],['limping','Limping'],['back_leg_drag','Dragging back legs'],['front_knees','On front knees'],['weak','Weak'])),
      Q('Legs and feet','Body · skin, body, legs','choice',o(['normal','Normal'],['arthritis','Swollen joint'],['fracture','Bone out of place'],['foot_rot','Rotten hoof'])),
      Q('Nervous signs','Body · skin, body, legs','multi',o(['none','Normal'],['circling','Circling'],['head_tilt','Head tilted'],['star_gazing','Head back'],['blind','Cannot see'],['tremors','Shivering'],['ataxia','Unsteady'])),
      Q('Udder','Final · female only','choice',o(['normal','Normal'],['swollen_hard','Hard and swollen'],['rashes','Rash'],['wound','Wound'],['lumps','Lumps'])),
      Q('Milk','Final · female only','choice',o(['no','No milk'],['milk','Milk'],['colostrum','First milk'],['water','Watery'],['pus','Pus'])),
      Q('Milk test','Final · female only','choice',o(['pos','Positive'],['neg','Negative']),{hint:'Asked only when there is milk'}),
      Q('Back passage','Final · female only','choice',o(['none','Normal'],['lochia_normal','Clean discharge'],['discharge_bad_smell','Bad smell'],['pus','Pus'],['prolapse','Tissue hanging'])),
      Q('Passing urine','Final · male only','choice',o(['no','Normal'],['straining','Straining'],['no_urine','No urine'])),
      Q('Anything else','Final','multi',o(['none','None'],['red_urine','Red urine'],['edema','Swelling under the jaw'],['pushed','Pushed off feed'])),
      Q('Suckle test','Final · kid only','choice',o(['present','Sucks'],['absent','No suckle']),{hint:'Finger in the mouth'}),
      Q('Responsiveness','Final · kid only','choice',o(['alert','Alert'],['dull','Dull'],['unresponsive','Unresponsive'])),
      Q('Navel','Final · milk kid only','choice',o(['normal','Normal'],['wet','Wet'],['swollen','Swollen'],['painful','Painful'])),
      Q('Drop test','Final · milk kid only','choice',o(['spiderman','Lands like Spider-Man'],['barely','Barely stays up'],['falls','Falls'],['na','Not done']),{hint:'Land the kid from about 20 cm; not done when the kid is already down'}),
      Q('Milk bar','Final · milk kid only','multi',o(['normal','Drinking'],['not_drinking','Not drinking']),{hint:'K2 free-choice milk bar only'}),
      Q('Feeds refused today','Final · kid only','number',null,{min:0,max:3,required:false,hint:'Required for K1 (3 bar sessions) and weaning kids (2 bottles)'}),
      Q('Which feed','Final · kid only','choice',o(['1','Feed 1'],['2','Feed 2'],['3','Feed 3']),{required:false})];
    f.chain(ids);
    const ap=f.add('approval',{label:'Health Director reviews the proposed assessment',roleId:'role_director_health',outcomes:['Approve treatment','Treat none of these']});
    const tr=f.add('action',{label:'Treat per Health protocol',link:'#/health/config',linkLabel:'Health protocol'});
    const e=f.add('end',{label:'Treatment approved',outcome:'done'});
    const cl=f.add('end',{label:'Assessment closed',outcome:'rejected'});
    f.link(ids[ids.length-1],ap); f.link(ap,tr,'Approve treatment'); f.link(ap,cl,'Treat none of these'); f.link(tr,e);
    return f.nodes;
  }

  function destPrepFlow(){
    const f=flow();
    const s=f.add('start',{label:'Transit started'});
    const pen=f.add('question',{label:'Destination pen',answer:'ref',refColl:'pens',required:true,page:'Allocate'});
    const par=f.add('parallel',{label:'Prepare in parallel'});
    const e1=f.add('evidence',{label:'Pen emptied',media:['video'],min:1,max:2,lane:'Pen'});
    const e2=f.add('evidence',{label:'Pen sanitized',media:['video'],min:1,max:2,lane:'Pen'});
    const w1=f.add('action',{label:'Fill water with ORS',lane:'Water'});
    const w2=f.add('evidence',{label:'Water trough with ORS',media:['photo'],min:1,max:3,lane:'Water'});
    const j=f.add('join',{label:'Both lanes done'});
    const ap=f.add('approval',{label:'Pen ready',roleId:'role_park_head',outcomes:['Ready','Redo']});
    const e=f.add('end',{label:'Destination ready',outcome:'done'});
    f.link(s,pen); f.link(pen,par); f.link(par,e1,'Pen'); f.link(e1,e2); f.link(par,w1,'Water'); f.link(w1,w2); f.link(e2,j); f.link(w2,j); f.link(j,ap); f.link(ap,e);
    return f.nodes;
  }

  /* Transit = Drive "Transit-SOP" v1 (2026-09-15): Phase 1 briefing + certificates, Phase 2 Viruflex-S disinfection the day before,
     Phase 3 weighbridge References 1/2 before payment, load limits, loading order, Phase 4 checks every 3 h (video) + 48 h medicine,
     Phase 5 pit stop every 12 h (4-6 h rest), Phase 6 unloading. Max 72 h (set_travel_days = 3 days). No truck register exists in stg. */
  function transitFlow(){
    const f=flow();
    const vid=(label,page,p)=>f.add('evidence',Object.assign({label,media:['video'],min:1,max:5,page},p||{}));
    const s=f.add('start',{label:'Boarding list approved'});
    const certs=f.add('question',{label:'Certificates in hand',answer:'multi',options:[{v:'vet',l:'Veterinary certificate'},{v:'eway',l:'Transit certificate (E-Way Bill)'},{v:'invoice',l:'Invoice'}],required:true,page:'Pre-dispatch',hint:'All three are required'});
    const brA=f.add('question',{label:'Briefing audio call recording sent to Central Office?',answer:'choice',options:YN,required:true,page:'Pre-dispatch',hint:'Required before truck booking; briefing at least 30 minutes',reject:{op:'=',value:'no',reason:'No briefing recording: do not book the truck'}});
    const brV=vid('Standup briefing video with all transit staff','Pre-dispatch');
    const veh=f.add('question',{label:'Vehicle type',answer:'choice',options:[{v:'bolero',l:'Bolero Pickup (up to 40 animals)'},{v:'eicher',l:'Eicher (41 to 100 animals)'}],required:true,page:'Day before loading'});
    const dis=vid('Vehicle disinfection video (Viruflex-S 5 ml per litre: interiors, wheels, exterior)','Day before loading');
    const med=vid('Medicine and equipment stock video','Day before loading');
    const hay=vid('Hay bed video','Day before loading');
    const rain=vid('Rain cover and ventilation video','Day before loading');
    const kind=f.add('question',{label:'Load type',answer:'choice',options:[{v:'fattening',l:'Fattening'},{v:'breeding',l:'Breeding stock'}],required:true,page:'Loading'});
    const c=f.add('question',{label:'Animals boarded',answer:'number',unit:'animals',min:0,required:true,page:'Loading',hint:'Count every animal while loading',rule:{maxFromSetting:{fattening:'set_load_max_fat',breeding:'set_load_max_breed'},minFromSetting:{fattening:'set_load_min_fat'}}});
    const dk=f.add('decision',{label:'Breeding stock?',q:kind,op:'=',value:'breeding'});
    const dF=f.add('decision',{label:'More than the fattening max (80)?',q:c,op:'>',value:80,valueSetting:'set_load_max_fat'});
    const dFmin=f.add('decision',{label:'Fewer than the fattening min (75)?',q:c,op:'<',value:75,valueSetting:'set_load_min_fat'});
    const under=f.add('end',{label:'Under the load minimum: add animals before dispatch',outcome:'rejected'});
    const dB=f.add('decision',{label:'More than the breeding max (50)?',q:c,op:'>',value:50,valueSetting:'set_load_max_breed'});
    const over=f.add('end',{label:'Over the load limit: reduce the load',outcome:'rejected'});
    const order=f.add('action',{label:'Load in order: heavily pregnant (lower deck, near cabin) → pregnant (upper, near cabin) → non-pregnant females → bucks; males and females separated',page:'Loading'});
    const load=vid('Loading video (truck inside a boundary, door gap filled with sacks)','Loading');
    const r1=f.add('question',{label:'Reference 1: empty vehicle and vehicle with empty-stomach animals',answer:'number',unit:'kg',min:0,required:true,page:'Weighbridge'});
    const r2=f.add('question',{label:'Reference 2: vehicle + animals at a different weighbridge on the route',answer:'number',unit:'kg',min:0,required:true,page:'Weighbridge',hint:'Payment is released only after Reference 2 is confirmed'});
    const pay=f.add('approval',{label:'Central Office confirms Reference 2 before payment',roleId:'role_director_procurement',outcomes:['Release payment','Hold payment']});
    const chk=f.add('repeat',{label:'Transit check: no stampede',every:3,everyUnit:'hours',everySetting:'set_travel_check',forAmount:3,forUnit:'days',forSetting:'set_travel_days',media:['video']});
    const pit=f.add('repeat',{label:'Pit stop: site video, unload and count, 4-6 h rest (males and females 50 m apart), fresh hay bed, count and reload video',every:12,everyUnit:'hours',everySetting:'set_pitstop_every',forAmount:3,forUnit:'days',forSetting:'set_travel_days',media:['video']});
    const inj=f.add('action',{label:'Every 48 h at a pit stop: chocolate injection after feed, then a 30-min water session; never inject and load',link:'#/health/config',linkLabel:'Health protocol'});
    const rc=f.add('question',{label:'Animals received',answer:'number',unit:'animals',min:0,required:true,page:'Arrival',hint:'Count every animal while unloading'});
    const un=vid('Unloading video (one person, one animal)','Arrival');
    const d=f.add('decision',{label:'Received less than boarded?',q:rc,op:'<',value:'@'+c,page:'Arrival'});
    const ver=f.add('approval',{label:'Central Office verifies the count and videos',roleId:'role_verifier',outcomes:['Approve','Rework']});
    const e=f.add('end',{label:'Arrived',outcome:'done'});
    f.chain([s,certs,brA,brV,veh,dis,med,hay,rain,kind,c,dk]);
    f.link(dk,dB,'Yes'); f.link(dk,dF,'No'); f.link(dF,over,'Yes'); f.link(dB,over,'Yes'); f.link(dF,dFmin,'No'); f.link(dFmin,under,'Yes'); f.link(dFmin,order,'No'); f.link(dB,order,'No');
    f.chain([order,load,r1,r2,pay,chk,pit,inj,rc,un,d]); f.link(d,ver,'Yes'); f.link(d,e,'No'); f.link(ver,e);
    return f.nodes;
  }

  /* ---- Published goatos-stg SOPs (sop_versions.status='published', form_dsl transcribed read-only 2026-09-17) ---- */
  const YN=[{v:'yes',l:'Yes'},{v:'no',l:'No'}];
  function verifyEnd(f,from,label){
    const a=f.add('approval',{label:label||'Verifier reviews the evidence',roleId:'role_verifier',outcomes:['Approve','Rework']});
    const ok=f.add('end',{label:'Evidence verified',outcome:'done'});
    const re=f.add('end',{label:'Evidence rework',outcome:'rejected'});
    f.link(from,a); f.link(a,ok,'Approve'); f.link(a,re,'Rework');
    return a;
  }

  /* counts.birth v1: recording form → Counts approver → follow-up tracks (kid = K0 SOP, mother) → evidence review */
  function birthFlow(){
    const f=flow();
    const s=f.add('start',{label:'Kid born'});
    const ids=[s,
      f.add('question',{label:'Mother RFID',answer:'scan',required:true,page:'Record birth',hint:'Must resolve to a female on the herd register'}),
      f.add('question',{label:'Litter size',answer:'choice',options:[{v:'1',l:'1'},{v:'2',l:'2'},{v:'3',l:'3'}],required:true,page:'Record birth',hint:'One register entry per kid, each with a provisional tag'}),
      f.add('question',{label:'Birth location (pen)',answer:'ref',refColl:'pens',required:true,page:'Record birth'}),
      f.add('question',{label:'Date of birth',answer:'date',required:true,page:'Record birth'}),
      f.add('question',{label:'Time of birth (optional)',answer:'text',required:false,page:'Record birth',hint:'24-hour IST, e.g. 06:45; unknown falls back to 07:00'}),
      f.add('question',{label:'Species',answer:'choice',options:[{v:'goat',l:'Goat'},{v:'sheep',l:'Sheep'}],required:true,page:'Record birth'}),
      f.add('question',{label:'Breed',answer:'text',required:true,page:'Record birth'}),
      f.add('question',{label:'Sex',answer:'choice',options:[{v:'female',l:'Female'},{v:'male',l:'Male'}],required:true,page:'Record birth'}),
      f.add('question',{label:'Birth weight',answer:'number',unit:'kg',min:0,required:false,page:'Record birth'}),
      f.add('evidence',{label:'Birth proof video (live camera)',media:['video'],min:1,max:1,page:'Record birth',hint:'Newborn(s) with the mother; no gallery or import'}),
      f.add('question',{label:'1st colostrum given (immediately after delivery)',answer:'choice',options:YN,required:true,page:'Record birth'}),
      f.add('question',{label:'Notes',answer:'text',required:false,page:'Record birth'})];
    f.chain(ids);
    const ap=f.add('approval',{label:'Counts approver review',roleId:'role_counts_approver',outcomes:['Approve','Reject']});
    const rej=f.add('end',{label:'Rejected',outcome:'rejected'});
    const par=f.add('parallel',{label:'Birth follow-up tracks'});
    const kid=f.add('child',{label:'Kid track (K0 newborn care)',sopId:'sop_k0',lane:'Kid'});
    const M=(label,answer,p)=>f.add(answer?'question':'action',Object.assign({label,lane:'Mother'},answer?{answer,options:YN,required:true}:{},p||{}));
    const mv=label=>f.add('evidence',{label,media:['video'],min:1,max:1,lane:'Mother'});
    const m=[M('Are any babies still inside?','choice'),mv('Babies-inside check video'),
      M('Is the mother licking her babies?','choice'),mv('Mother licking video'),
      M("Mother's medicine",null,{hint:'Chocolate Injection 1.5 ml SQ · Meloxicam Paracetamol 4 ml IM · Exapar 20 ml · Glucoboost 100 ml mixed with 150 g concentrate'}),mv("Mother's medicine video"),
      M('ORS water (1st round)'),mv('ORS water video'),
      M('Is the mother eating?','choice'),mv('Mother eating video'),
      f.add('wait',{label:'Exactly 50 min after the 1st ORS round',amount:50,unit:'minutes',lane:'Mother'}),
      M('ORS water (2nd round)'),mv('ORS water 2nd round video')];
    for(let i=0;i<m.length-1;i++)f.link(m[i],m[i+1]);
    const j=f.add('join',{label:'Both tracks done'});
    f.link(ids[ids.length-1],ap); f.link(ap,par,'Approve'); f.link(ap,rej,'Reject');
    f.link(par,kid,'Kid'); f.link(par,m[0],'Mother'); f.link(kid,j); f.link(m[m.length-1],j);
    verifyEnd(f,j,'Birth evidence review (one item per mother or child)');
    return f.nodes;
  }

  /* counts.death v1: exactly two operator video steps → Counts approver → media verification */
  function deathFlow(){
    const f=flow();
    const s=f.add('start',{label:'Animal found dead'});
    const ids=[s,
      f.add('question',{label:'Animal RFID',answer:'scan',required:true,page:'Death'}),
      f.add('question',{label:'What happened',answer:'text',required:true,page:'Death',hint:'3 to 500 characters'}),
      f.add('question',{label:'When the death occurred',answer:'date',required:false,page:'Death',hint:'Defaults to the time recorded'}),
      f.add('evidence',{label:'Death video (live camera, ear tag visible)',media:['video'],min:1,max:1,page:'Death video'}),
      f.add('evidence',{label:'Post-mortem video (live camera, timestamp visible, one continuous take)',media:['video'],min:1,max:1,page:'Post-mortem video'})];
    f.chain(ids);
    const ap=f.add('approval',{label:'Counts approver review',roleId:'role_counts_approver',outcomes:['Approve','Reject']});
    const rej=f.add('end',{label:'Rejected',outcome:'rejected'});
    const applied=f.add('action',{label:'Animal exited as dead; open work cancelled'});
    f.link(ids[ids.length-1],ap); f.link(ap,applied,'Approve'); f.link(ap,rej,'Reject');
    verifyEnd(f,applied,'Media verification');
    return f.nodes;
  }

  /* counts.reconcile v1: return a stray animal to its registered pen, verified before apply */
  function reconcileFlow(){
    const f=flow();
    const ids=[f.add('start',{label:'Animal scanned in the wrong pen'}),
      f.add('question',{label:'Animal RFID',answer:'scan',required:true,page:'Reconcile',hint:'The tag exactly as the weighing operator scanned it'}),
      f.add('question',{label:'Registered pen',answer:'ref',refColl:'pens',required:true,page:'Reconcile'}),
      f.add('evidence',{label:'Return to registered pen video (ear tag inside that pen)',media:['video'],min:1,max:1,page:'Pen return'})];
    f.chain(ids); verifyEnd(f,ids[ids.length-1]);
    return f.nodes;
  }

  /* milk.preparation v1: one preparation per farm per day (feeds tomorrow); goat-milk steps only when goat milk is used */
  function milkPrepFlow(){
    const f=flow();
    const P='Preparation';
    const num=(label,unit,req,p)=>f.add('question',Object.assign({label,answer:'number',unit,min:0,required:req,page:P},p||{}));
    const v=label=>f.add('evidence',{label,media:['video'],min:1,max:1,page:P});
    const s=f.add('start',{label:"Read the day's milk plan"});
    const farm=f.add('question',{label:'Farm',answer:'ref',refColl:'parks',required:true,page:P});
    const am=num('Goat milk collected · morning','litres',true,{hint:'May be zero'});
    const pm=num('Goat milk collected · evening','litres',true);
    const used=f.add('question',{label:'Goat milk used in this preparation',answer:'choice',options:YN,required:true,page:P});
    const d=f.add('decision',{label:'Goat milk used?',q:used,op:'=',value:'yes'});
    const g=[num('Goat milk quantity','litres',true,{page:'Goat milk'}),f.add('evidence',{label:'Goat milk quantity video',media:['video'],min:1,max:1,page:'Goat milk'}),
      num('Boiling temperature','°C',true,{page:'Goat milk'}),f.add('evidence',{label:'Boiling temperature video',media:['video'],min:1,max:1,page:'Goat milk'}),
      num('Cooled temperature','°C',true,{page:'Goat milk'}),f.add('evidence',{label:'Cooled temperature video',media:['video'],min:1,max:1,page:'Goat milk'})];
    const u=[num('UHT milk quantity','litres',true,{hint:'Recorded into the UHT Milk feed stock on approval'}),v('UHT milk quantity video'),
      num('Citric acid','g',true,{hint:'Plan: 5.5 g per litre of prepared milk'}),v('Citric acid mixing video')];
    f.chain([s,farm,am,pm,used,d]); f.link(d,g[0],'Yes'); f.link(d,u[0],'No'); f.chain(g); f.link(g[g.length-1],u[0]); f.chain(u);
    verifyEnd(f,u[u.length-1],'Verifier review · one farm-day item with every step video');
    return f.nodes;
  }

  /* vaccination.drive v1: scan each goat when the vaccine is given; 1-5 pen videos (camera or gallery); verified before apply */
  function vaccinationDriveFlow(){
    const f=flow();
    const ids=[f.add('start',{label:'Protocol window due'}),
      f.add('question',{label:'Goats vaccinated',answer:'scan',required:true,page:'Vaccinate',hint:'Scan each goat exactly when the vaccine is given; the scan time is the vaccination time'}),
      f.add('evidence',{label:'Pen proof videos',media:['video'],min:1,max:5,page:'Pen video',hint:'Camera or gallery, up to 5 videos'})];
    f.chain(ids); verifyEnd(f,ids[ids.length-1],'Proof verification');
    return f.nodes;
  }

  function sop(id,dept,title,nodes,v,category){
    const at='2026-09-'+String(2+((v*7)%13)).padStart(2,'0')+'T09:30:00Z';
    return {id,dept,title,category:category||'action',nodes,versions:v?[{v,at,by:'CEO / CXO',nodes:JSON.parse(JSON.stringify(nodes)),running:0}]:[],status:'active'};
  }
  /* Proposed SOP: not in goatos-stg and no Android screen yet, so never published and nothing running. */
  function draft(id,dept,title,nodes){
    return {id,dept,title,category:'action',nodes,versions:[],status:'draft',running:0,source:'Proposed · not in the app yet'};
  }

  window.buildSeed=function(){
    const st={meta:{v:3,created:new Date().toISOString()}};
    st.farms=[
      {id:'farm_cbe',code:'CBE',name:'Coimbatore',kind:'core',status:'active'},
      {id:'farm_cpt',code:'CPT',name:'Channapatna',kind:'core',status:'active'}];
    /* stg locations(type=park) codes CBE / CPT */
    st.parks=[
      {id:'park_cbe',code:'CBE',name:'Coimbatore',farmId:'farm_cbe',status:'active'},
      {id:'park_cpt',code:'CPT',name:'Channapatna',farmId:'farm_cpt',status:'active'}];
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
      st.pens.push({id,parkId:pk,name,displayName:name,capacity:'',stageId:prof?stg('sp_goat',prof):'',sex:'',hasIcu:false,lifecycleStatus:'',status:'active'});
      parts.forEach(pt=>st.partitions.push({id:id+'_p'+pt.replace(/\D/g,''),penId:id,name:pt,capacity:'',status:'active'}));
    }));
    /* Pen names repeat across parks (Castro, Gandhi, ...): displayName adds the park code where a name collides. */
    st.pens.forEach(p=>{if(st.pens.some(q=>q!==p&&q.name===p.name))p.displayName=p.name+' · '+st.parks.find(k=>k.id===p.parkId).code;});

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

    /* items = goatos-stg inventory_items (7 active rows, all category 'vaccine', base unit dose). Feed items live in Feed Config;
       medicines are named inside Health Config protocols, not in inventory_items. */
    st.categories=[{id:'cat_vac',parentId:'',name:'Vaccines',status:'active'}];
    st.items=[['VAC-BT','Blue Tongue vaccine'],['VAC-ET-TT','ET+TT vaccine'],['VAC-FMD','FMD vaccine'],['VAC-GP','Goat Pox vaccine'],['VAC-HS','HS vaccine'],['VAC-PPR','PPR vaccine'],['VAC-SP','Sheep Pox vaccine']]
      .map(([code,name],i)=>({id:'itm_vac'+i,categoryId:'cat_vac',code,name,unit:'dose',depts:['Preventive Care'],status:'active'}));

    /* RBAC roles = goatos-stg org_role_catalog (tier x vertical rows + the legacy flat roles still granted). The six legacy
       '<vertical>_director' duplicates of director_<vertical> are left out. Job titles are a separate list: st.designations (seed-refs). */
    const GRADE={am:'Assistant Manager',manager:'Manager',head:'Head',director:'Director',ceo_cxo:'CEO / CXO',cxo:'CEO / CXO'}; window.GRADE_LABELS=GRADE;
    st.roles=[['am_breeding','Assistant Manager · Breeding','am',0,''],['am_feed','Assistant Manager · Feed','am',0,''],['am_growth','Assistant Manager · Growth','am',0,''],['am_health','Assistant Manager · Health','am',0,''],['am_infrastructure','Assistant Manager · Infrastructure','am',0,''],['am_milk','Assistant Manager · Milk','am',0,''],['am_preventive_care','Assistant Manager · Preventive Care','am',0,''],['am_procurement','Assistant Manager · Procurement','am',0,''],['am_sales','Assistant Manager · Sales','am',0,''],['director_breeding','Director · Breeding','director',0,''],['director_feed','Director · Feed','director',0,''],['director_growth','Director · Growth','director',0,''],['director_health','Director · Health','director',0,''],['director_infrastructure','Director · Infrastructure','director',0,''],['director_milk','Director · Milk','director',0,''],['director_preventive_care','Director · Preventive Care','director',0,''],['director_procurement','Director · Procurement','director',0,''],['director_sales','Director · Sales','director',0,''],['head_breeding','Head · Breeding','head',0,'Head of operations'],['head_feed','Head · Feed','head',0,'Head of operations'],['head_growth','Head · Growth','head',0,'Head of operations'],['head_health','Head · Health','head',0,'Head of operations'],['head_infrastructure','Head · Infrastructure','head',0,'Head of operations'],['head_milk','Head · Milk','head',0,'Head of operations'],['head_preventive_care','Head · Preventive Care','head',0,'Head of operations'],['head_procurement','Head · Procurement','head',0,'Head of operations'],['head_sales','Head · Sales','head',0,'Head of operations'],['manager_breeding','Manager · Breeding','manager',0,''],['manager_feed','Manager · Feed','manager',0,''],['manager_growth','Manager · Growth','manager',0,''],['manager_health','Manager · Health','manager',0,''],['manager_infrastructure','Manager · Infrastructure','manager',0,''],['manager_milk','Manager · Milk','manager',0,''],['manager_preventive_care','Manager · Preventive Care','manager',0,''],['manager_procurement','Manager · Procurement','manager',0,''],['manager_sales','Manager · Sales','manager',0,''],['procurement_manager','Procurement Manager','manager',0,''],['ceo_internal','CEO / CXO','ceo_cxo',1,'Founder / builder cohort. Older flat role, granted before the org role model.'],['counts_approver','Counts Approver','director',1,'Named approver for birth and death counts.'],['hr','HR','director',1,''],['market_reporter','Market Reporter','director',1,''],['operator','Operator','am',1,'Ground executor. Older flat role.'],['park_head','Park Head','head',1,'Older flat role, granted before the org role model.'],['toxin_tester','Toxin Tester','director',1,''],['verifier','Verifier','director',1,'Video verification team across departments. Older flat role.']]
      .map(r=>({id:'role_'+r[0],key:r[0],name:r[1],grade:r[2],gradeLabel:GRADE[r[2]]||'',legacy:!!r[3],note:r[4],status:'active'}));
    /* Anonymous people: designation + number only. */
    st.people=[
      {id:'ppl_ceo1',name:'CEO / CXO 01',roleId:'role_ceo_internal',parkIds:[],phone:'',status:'active'},
      {id:'ppl_op1',name:'Operator 01',roleId:'role_operator',parkIds:['park_cpt'],phone:'',status:'active'},
      {id:'ppl_op2',name:'Operator 02',roleId:'role_operator',parkIds:['park_cbe'],phone:'',status:'active'},
      {id:'ppl_ver1',name:'Verifier 01',roleId:'role_verifier',parkIds:['park_cpt','park_cbe'],phone:'',status:'active'},
      {id:'ppl_ph1',name:'Park Head 01',roleId:'role_park_head',parkIds:['park_cpt'],phone:'',status:'active'},
      {id:'ppl_ca1',name:'Counts Approver 01',roleId:'role_counts_approver',parkIds:[],phone:'',status:'active'}];
    /* Approvers per published stg workflow (approval / proof_verification nodes). */
    st.approvers=[
      ['Procurement','Animal selection (CEO decides on the web)','role_ceo_internal'],['Procurement','Final boarding list','role_ceo_internal'],['Procurement','Reference 2 weighbridge before payment','role_director_procurement'],['Procurement','Warm-up release','role_ceo_internal'],
      ['Weighing','Weight and video','role_verifier'],['Feed','Distribution proof','role_verifier'],['Counts','Birth','role_counts_approver'],['Counts','Death','role_counts_approver'],['Counts','Shifting','role_park_head'],
      ['Counts','Evidence review','role_verifier'],['Milk','Session and preparation proof','role_verifier'],['Preventive Care','Vaccination proof','role_verifier'],
      ['Health','Assessment approval','role_director_health']].map((a,i)=>({id:'apr_'+i,dept:a[0],step:a[1],roleId:a[2],status:'active'}));

    /* Read-only sample of the Procurement vendor register (goatos-stg procurement_vendors: 674 rows, 609 active). Business names and
       cities as stored; record_type as 'supplies'. No truck/vehicle table exists in stg, so there is no truck seed. */
    st.vendors=[['Company','Lenatural','Coimbatore'],['Company','Venkateshwara','Coimbatore'],['Pellet Factory','Hindustan Feedscare Pvt Ltd','Erode'],
      ['Pellet Factory','Kamadhenu Feeds (P) Limited','Vijayawada'],['Pellet Factory','Vallabha Feeds Pvt Ltd','Narasaraopet'],['Feed Agent','Mishka Cattle Feeds','Coimbatore'],
      ['Feed Agent','Standard Growth Agri Products','Erode'],['Feed Agent','Shri Bannaramma Agro Industries','Mandya'],['Feed Agent','GS Silage','Kinathukadavu'],
      ['Goat Farm','Renuka Goat Farm','Pune'],['Goat Farm','Sawant Naad Goat Farm','Pune'],['Goat Farm','Shanthan Goat Farm','Bengaluru'],
      ['Goat Stockist','Bhopal Goat And Agro','Bhopal'],['Goat Stockist','Gokul Agronomics','Bhopal'],['Goat Stockist','Goat World Farm','Hanumangarh'],
      ['Transport Agent','Aashka Logistics Solution','Bhopal'],['Transport Agent','Arya Logistics','Bhopal'],['Transport Agent','Best Road Carriers','Bhopal'],
      ['Test Lab','EKA Eureka Lab','Bengaluru'],['Test Lab','Mettex Labs Pvt Ltd','Chennai'],['UHT Milk Supplier','Doddla Milk','Coimbatore'],
      ['Veterinary Accessories','Cattle Garage','Coimbatore'],['Insurance','Iffco Tokio General Insurance','Coimbatore']]
      .map(([t,n,c],i)=>({id:'ven_'+(i+1),name:n,supplies:t,farmId:'',city:c,phone:'',status:'active'}));
    st.vendorSummary={total:674,active:609,activeByType:{'Butcher':182,'Farmer':109,'Sheep Agent':87,'Agent':57,'Transport Agent':54,'Feed Agent':25,'Sheep Stockist':12,'Company':10,
      'Pellet Factory':8,'Goat Stockist':7,'Breeding Agent':7,'Manure Agent':7,'Labor Agent':6,'Goat Farm':5,'Test Lab':4,'Feed Stockist':4,'Insurance':4,'UHT Milk Supplier':4,
      'Veterinary Accessories':4,'Chain Link Mesh Contractor':3,'Solar Light Supplier':3,'Flooring Mat':2,'Grain Supplier':2,'Vet Doctor':1,'Goats Agent':1,'Grass Cutter':1}};

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
      set('set_travel_check','Procurement','Transit check interval',3,'hours'),set('set_pitstop_every','Procurement','Pit stop interval',12,'hours'),
      set('set_load_min_fat','Procurement','Min load · fattening',75,'animals'),set('set_load_max_fat','Procurement','Max load · fattening',80,'animals'),set('set_load_max_breed','Procurement','Max load · breeding stock',50,'animals'),set('set_warmup','Procurement','Warm-up period',14,'days'),
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
      ['approval',{label:'Final boarding list',roleId:'role_ceo_internal',outcomes:['Approve','Send back']}],
      ['evidence',{label:'Truck sanitized',media:['video'],min:1,max:2,page:'Truck'}],
      ['question',{label:'Familiar feed packed',answer:'number',unit:'kg',min:0,required:true,page:'Truck'}],
      ['end',{label:'Ready to depart',outcome:'done'}]]);
    const warmup=linear([
      ['start',{label:'Arrived at destination'}],
      ['question',{label:'Animals received into pen',answer:'number',unit:'animals',min:0,required:true,page:'Arrival'}],
      ['action',{label:'Feed transition per Feed config',link:'#/feed/config',linkLabel:'Feed config',page:'Arrival'}],
      ['repeat',{label:'Daily warm-up check',every:24,everyUnit:'hours',forAmount:14,forUnit:'days',forSetting:'set_warmup',media:['photo']}],
      ['approval',{label:'Release to herd',roleId:'role_ceo_internal',outcomes:['Release','Extend warm-up']}],
      ['end',{label:'Released',outcome:'done'}]]);
    const removal=linear([
      ['start',{label:'Pen scheduled for weighing tomorrow'}],
      ['question',{label:'Pen',answer:'ref',refColl:'pens',required:true,page:'Pen'}],
      ['evidence',{label:'Feed removal video',media:['video'],min:1,max:1,page:'Removal'}],
      ['evidence',{label:'Water removal video',media:['video'],min:1,max:1,page:'Removal'}],
      ['approval',{label:'Verifier reviews removal',roleId:'role_verifier',outcomes:['Approve','Send back']}],
      ['end',{label:'Pen ready for weighing',outcome:'done'}]]);
    /* stg feed.direction v1: weight photo, distribution video, water video, plus the feed.wastage leftover-feed video; verified before apply */
    const feedDist=linear([
      ['start',{label:'Session due'}],
      ['question',{label:'Pen',answer:'ref',refColl:'pens',required:true,page:'Session'}],
      ['question',{label:'Session',answer:'number',min:1,required:true,page:'Session',hint:'Morning / evening per the session template'}],
      ['evidence',{label:'Feed weight photo (on the scale, before it is given out)',media:['photo'],min:1,max:1,page:'Proof'}],
      ['evidence',{label:'Feed distribution video',media:['video'],min:1,max:1,page:'Proof'}],
      ['evidence',{label:'Water distribution video',media:['video'],min:1,max:1,page:'Proof',hint:'Video only: a photo of a full trough does not prove it was filled today'}],
      ['evidence',{label:'Leftover feed video (before the trough is cleared)',media:['video'],min:1,max:1,page:'Wastage',hint:'The verifier reads the leftover weight off the clip'}],
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

    /* stg shifting v2: every movement goes to Park Head approval first; low priority is due next day (raised at/after 13:30 IST: the day
       after); high priority adds feed packing + feeding videos; completion video; verifier evidence review. */
    const shf=flow();
    (function(f){
      const s=f.add('start',{label:'Animals need to move pens'});
      const cat=f.add('question',{label:'Category',answer:'ref',refColl:'movementReasons',required:true,page:'Raise',hint:'Options come from the Movement reasons list'});
      const pri=f.add('question',{label:'Priority',answer:'choice',options:[{v:'low',l:'Low'},{v:'high',l:'High'}],required:true,page:'Raise'});
      const an=f.add('question',{label:'Animals',answer:'scan',required:true,page:'Raise'});
      const from=f.add('question',{label:'Source pen',answer:'ref',refColl:'pens',required:true,page:'Raise'});
      const to=f.add('question',{label:'Destination pen',answer:'ref',refColl:'pens',required:true,page:'Raise',hint:'Pen moves are within one park only'});
      const tag=f.add('question',{label:'Tag on arrival',answer:'choice',options:[{v:'destination_stage',l:'Adopt the destination pen tag'},{v:'keep_current',l:'Keep the current tag'}],required:true,page:'Raise'});
      const why=f.add('question',{label:'Why are the animals being shifted',answer:'text',required:false,page:'Raise'});
      const ap=f.add('approval',{label:'Park Head approval',roleId:'role_park_head',outcomes:['Approve','Reject']});
      const rej=f.add('end',{label:'Rejected by Park Head',outcome:'rejected'});
      const d2=f.add('decision',{label:'High priority?',q:pri,op:'=',value:'high'});
      const w=f.add('wait',{label:'Due next day (raised at or after 13:30 IST: the day after)',amount:1,unit:'days'});
      const fp=f.add('evidence',{label:'Feed packing video (high priority)',media:['video'],min:1,max:1,page:'Feed'});
      const fd=f.add('evidence',{label:'Feeding video (high priority)',media:['video'],min:1,max:1,page:'Feed'});
      const v=f.add('evidence',{label:'Shifting video (ear tags visible inside the destination pen)',media:['video'],min:1,max:1,page:'Complete'});
      const vr=f.add('approval',{label:'Verifier reviews the evidence',roleId:'role_verifier',outcomes:['Approve','Rework']});
      const e=f.add('end',{label:'Evidence verified',outcome:'done'});
      const re=f.add('end',{label:'Evidence rework',outcome:'rejected'});
      f.chain([s,cat,pri,an,from,to,tag,why,ap]); f.link(ap,d2,'Approve'); f.link(ap,rej,'Reject');
      f.link(d2,fp,'Yes'); f.link(d2,w,'No'); f.link(fp,fd); f.link(fd,v); f.link(w,v); f.link(v,vr); f.link(vr,e,'Approve'); f.link(vr,re,'Rework');
    })(shf);
    /* K0 = stg counts.birth follow_up kid track (the mother track runs in the Birth SOP). Every step carries one video. */
    const k0=(function(){
      const f=flow(), yn=[{v:'yes',l:'Yes'},{v:'no',l:'No'}];
      const step=(label,answer,page,p)=>[answer?f.add('question',Object.assign({label,answer,required:true,page},answer==='choice'?{options:yn}:{},p||{})):f.add('action',Object.assign({label,page},p||{})),
        f.add('evidence',{label:label.replace(/\?$/,'')+' video',media:['video'],min:1,max:1,page})];
      const R='Right after birth';
      const ids=[f.add('start',{label:'Kid born · K0 - Newborn'}),
        ...step('Is the kid clean?','choice',R,{hint:'Cleaned and dried after delivery'}),
        ...step('Iodine dipping of umbilical cord',null,R),
        ...step('Are the front teeth outside the lower gum?','choice',R),
        ...step('Does the kid have a suck reflex?','choice',R,{hint:'Clean finger in the mouth; it should start sucking'}),
        ...step('1st colostrum',null,R),
        ...step('Kid weight','number',R,{unit:'kg',min:0}),
        f.add('question',{label:'Record pen',answer:'ref',refColl:'pens',required:false,page:R,hint:'Only when the park has no kid pen yet; this pen becomes the kid pen'}),
        f.add('wait',{label:'1 hour after birth',amount:60,unit:'minutes'}),
        ...step('Is the kid standing?','choice','+1 hour'),
        f.add('repeat',{label:'Colostrum (2nd onwards)',times:['07:00','11:00','15:00','18:30','22:00'],every:1,everyUnit:'days',forAmount:2,forUnit:'days',slotCount:10,notifySetting:'set_colostrum_notify',media:['video']}),
        f.add('wait',{label:'Until 07:00 on day 2 (after all colostrum rounds)',amount:2,unit:'days'}),
        f.add('question',{label:'Tag the kid: permanent RFID',answer:'scan',required:true,page:'Tag the kid',hint:'The temporary identifier is retired; same goat record'}),
        f.add('evidence',{label:'Tagging video',media:['video'],min:1,max:1,page:'Tag the kid'}),
        f.add('child',{label:'Shift to K1 (Growth)',sopId:'sop_shift'}),
        f.add('end',{label:'Moved to K1',outcome:'done'})];
      f.chain(ids); return f.nodes;
    })();
    /* K1 milk training runs the stg milk.feeding v1 session form (4 sessions a day, refusal watchlist, two distinct videos). */
    const k1=(function(){
      const f=flow(), S='Feeding session';
      const ids=[f.add('start',{label:'Kid in K1 - Milk Training'}),
        f.add('question',{label:'Farm',answer:'ref',refColl:'parks',required:true,page:S}),
        f.add('question',{label:'Feeding session',answer:'number',min:1,max:4,required:true,page:S,hint:'One of the four daily sessions; each is its own task and verification'}),
        f.add('question',{label:'Watchlist: did each listed kid drink?',answer:'choice',options:[{v:'yes',l:'Yes'},{v:'no',l:'No'}],required:true,page:S,hint:'Two verified Yes answers in a row take a kid off the list; a No resets it'}),
        f.add('question',{label:'New refusals this session',answer:'scan',required:false,page:S}),
        f.add('evidence',{label:'Clean bottles video',media:['video'],min:1,max:1,page:'Videos'}),
        f.add('evidence',{label:'Mixing and filling video',media:['video'],min:1,max:1,page:'Videos',hint:'Must be a different clip from clean bottles'})];
      f.chain(ids);
      const a=f.add('approval',{label:'Verifier review · one item per farm, date and session',roleId:'role_verifier',outcomes:['Approve','Rework']});
      const c=f.add('child',{label:'Shift to K2 (Growth)',sopId:'sop_shift'});
      const e=f.add('end',{label:'Moved to K2',outcome:'done'});
      const re=f.add('end',{label:'Rework: re-shoot',outcome:'rejected'});
      f.link(ids[ids.length-1],a); f.link(a,c,'Approve'); f.link(a,re,'Rework'); f.chain([c,e]);
      return f.nodes;
    })();
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
      sop('sop_inspect','Procurement','Animal purchase inspection',inspectionFlow(),7,'event'),
      draft('sop_taghold','Procurement','Tag, vaccinate and hold',tagHold),
      draft('sop_reinspect','Procurement','Reinspect and board',reinspect),
      draft('sop_transit','Procurement','Transit',transitFlow()),
      draft('sop_destprep','Procurement','Destination pen preparation',destPrepFlow()),
      draft('sop_warmup','Procurement','Warm-up and feed transition',warmup),
      sop('sop_weigh','Weighing','Individual weighing',weighingFlow(),2),
      sop('sop_weigh_lump','Weighing','Lump-sum weighing',lumpSumWeighingFlow(),1),
      sop('sop_removal','Weighing','Feed and water removal',removal,1),
      Object.assign(sop('sop_feeddist','Feed','Feed distribution',feedDist,1,'commodity'),{source:'goatos-stg feed.direction v1'}),
      sop('sop_feedpack','Feed','Feed packing',feedPack,1,'commodity'),
      sop('sop_feedtrans','Feed','Feed transport',feedTrans,1,'commodity'),
      Object.assign(sop('sop_fever','Health','Sick animal report',healthFlow(),1,'problem'),{source:'Android ObservationFormScreen.kt · DiagnosisProposalScreen.kt'}),
      Object.assign(sop('sop_k0','Milk','K0 newborn care (birth follow-up)',k0,1,'event'),{source:'goatos-stg counts.birth v1 follow_up kid track'}),
      Object.assign(sop('sop_k1','Milk','K1 milk training (milk feeding)',k1,1,'action'),{source:'goatos-stg milk.feeding v1'}),
      sop('sop_k2','Milk','K2 milk drinking',k2,1,'action'),
      sop('sop_k3','Milk','K3 weaning',k3,1,'action'),
      sop('sop_shift','Counts','Shifting',shf.nodes,2,'action'),
      Object.assign(sop('sop_birth','Counts','Birth recording',birthFlow(),1,'event'),{source:'goatos-stg counts.birth v1'}),
      Object.assign(sop('sop_death','Counts','Death recording',deathFlow(),1,'event'),{source:'goatos-stg counts.death v1'}),
      Object.assign(sop('sop_reconcile','Counts','Pen reconcile',reconcileFlow(),1,'problem'),{source:'goatos-stg counts.reconcile v1'}),
      Object.assign(sop('sop_milkprep','Milk','Milk preparation',milkPrepFlow(),1,'commodity'),{source:'goatos-stg milk.preparation v1'}),
      Object.assign(sop('sop_vacdrive','Preventive Care','Vaccination session',vaccinationDriveFlow(),1,'action'),{source:'goatos-stg vaccination.drive v1'})];

    st.masters=[{id:'mst_proc',dept:'Procurement',title:'Procurement: purchase → transit → warm-up',status:'active',
      versions:[{v:1,at:'2026-09-10T08:00:00Z',by:'CEO / CXO',running:0}],
      stages:[
        {id:'s0',label:'Seller inspection and selection',sopId:'sop_inspect',deps:[],approvalRoleId:'role_ceo_internal',waitDays:'',waitSetting:'',repeatHours:'',repeatSetting:''},
        {id:'s1',label:'Tag, vaccinate and hold',sopId:'sop_taghold',deps:[{stage:'s0',state:'approved'}],approvalRoleId:'',waitDays:15,waitSetting:'set_seller_hold',repeatHours:'',repeatSetting:''},
        {id:'s2',label:'Reinspect and board',sopId:'sop_reinspect',deps:[{stage:'s1',state:'completed'}],approvalRoleId:'role_ceo_internal',waitDays:'',waitSetting:'',repeatHours:'',repeatSetting:''},
        {id:'s3',label:'Transit',sopId:'sop_transit',deps:[{stage:'s2',state:'approved'}],approvalRoleId:'',waitDays:3,waitSetting:'set_travel_days',repeatHours:3,repeatSetting:'set_travel_check'},
        {id:'s4',label:'Prepare destination pens',sopId:'sop_destprep',deps:[{stage:'s3',state:'started'}],approvalRoleId:'',waitDays:'',waitSetting:'',repeatHours:'',repeatSetting:''},
        {id:'s5',label:'Warm-up and feed transition',sopId:'sop_warmup',deps:[{stage:'s3',state:'completed'},{stage:'s4',state:'completed'}],approvalRoleId:'role_ceo_internal',waitDays:14,waitSetting:'set_warmup',repeatHours:24,repeatSetting:''}],
      cohort:{offered:'',selected:'',boarded:'',received:''}}];
    return st;
  };
})();
