/* Reference lists seed (from staging catalogues: sop_categories, sop_task_types, status_definitions, goats exit CHECKs, sold weight bands) */
window.seedRefs=function(st){
  const mk=(arr,f)=>arr.map((x,i)=>Object.assign({id:f+i,status:'active'},x));
  st.sopCategories=mk([['commodity','Commodity'],['problem','Problem'],['event','Event'],['action','Action'],['equipment','Equipment / Asset']].map(x=>({code:x[0],name:x[1]})),'sc_');
  const tt=['administer','weigh','tag','inspect','verify','transfer','clean','schedule_next','photo_record','calculate','death_evidence','record_pen','record_yes_no','do_and_confirm','record_select','video_record','feed_colostrum','record_text','record_number','record_multiselect','return_to_pen'];
  const ans={record_yes_no:'yes_no',record_select:'select',record_multiselect:'multiselect',record_number:'number',weigh:'number',record_text:'text'};
  const TT={schedule_next:'Schedule next visit',photo_record:'Take a photo',death_evidence:'Death evidence',record_pen:'Record pen',record_yes_no:'Yes / no answer',do_and_confirm:'Do and confirm',record_select:'Pick one option',video_record:'Record a video',feed_colostrum:'Feed colostrum',record_text:'Text answer',record_number:'Number answer',record_multiselect:'Pick several options',return_to_pen:'Return to pen'};
  const human=c=>c.replace(/_/g,' ').replace(/^./,m=>m.toUpperCase());
  st.taskTypes=mk(tt.map(c=>({code:c,key:c,name:TT[c]||human(c),answer:ans[c]||'none'})),'tt_');
  const sd=[['lifecycle',['alive','dead','sold','merged','inactive']],['reproductive',['pregnant','non_pregnant','mother','milking','buck']],['health',['healthy','sick','under_treatment','recovering','icu','quarantine']]];
  st.statusDefs=[];sd.forEach(([ax,codes])=>codes.forEach(c=>st.statusDefs.push({id:'sd_'+ax+'_'+c,axis:ax,code:c,key:c,name:({non_pregnant:'Not pregnant',under_treatment:'Under treatment',icu:'ICU'})[c]||human(c),days:'',status:'active'})));
  st.exitReasons=mk(['Sold','Died','Culled','Transferred','Lost'].map(n=>({name:n})),'ex_');
  st.purposes=mk(['Breeding','Fattening'].map(n=>({name:n})),'pu_');
  /* goatos-stg shifting_events category CHECK + live rows (normal is the most common). The Shifting SOP Category question reads this list. */
  st.movementReasons=mk([['normal','Normal'],['growth','Growth'],['health','Health'],['breeding','Breeding'],['delivery','Delivery'],['spacing','Spacing'],['flushing','Flushing']].map(x=>({key:x[0],code:x[0],name:x[1]})),'mv_');
  st.weightBands=mk([['Under 20 kg','',20],['20–35 kg',20,35],['35–40 kg',35,40],['40 kg +',40,'']].map(x=>({speciesId:'',name:x[0],fromKg:x[1],toKg:x[2]})),'wb_');
  /* 03-hardcoded-config: entities that were code constants / CHECKs with no editor. */
  st.saleProducts=mk([['Goat','sp_goat'],['Sheep','sp_sheep'],['Manure',''],['Mixed','']].map(x=>({name:x[0],speciesId:x[1]})),'spd_'); /* sales/domain ProductTypes */
  st.costKinds=mk([['animal','Animal price'],['transport','Transport'],['booking','Booking'],['labour','Labour'],['transit','Transit expenses'],['transition_feed','Transition feed'],['other','Other']].map(x=>({code:x[0],key:x[0],name:x[1]})),'ck_'); /* procurement_load_cost_lines CK */
  /* goatos-stg identifier_policies phase1-identifier-v1: primary_allowed t/f/t/f, auto_link_allowed f everywhere; types stay code-owned */
  st.identifierPolicies=[{id:'idp_1',version:1,name:'Identifier policy v1',code:'phase1-identifier-v1',key:'phase1-identifier-v1',status:'active',types:[
    {code:'animal_identifier_1',key:'animal_identifier_1',name:'RFID 1',primaryAllowed:true,autoLink:false},{code:'animal_identifier_2',key:'animal_identifier_2',name:'RFID 2',primaryAllowed:false,autoLink:false},
    {code:'temporary_tag',key:'temporary_tag',name:'Temporary tag',primaryAllowed:true,autoLink:false},{code:'smart_ble_tag',key:'smart_ble_tag',name:'Smart BLE tag',primaryAllowed:false,autoLink:false}]}];
  /* goatos-stg designation_catalog (12): job titles. RBAC roles are st.roles (org_role_catalog) in seed.js */
  st.designations=mk([['ceo_internal','CEO / CXO','cxo'],['pc_director','Preventive Care Director','director'],['growth_director','Growth Director','director'],['feed_director','Feed Director','director'],
    ['health_director','Health Director','director'],['breeding_director','Breeding Director','director'],['procurement_director','Procurement Director','director'],['procurement_manager','Procurement Manager','manager'],
    ['park_head','Park Head','manager'],['verifier','Verifier',''],['operator','Operator',''],['hr','HR','director']].map(x=>({code:x[0],key:x[0],name:x[1],grade:x[2],gradeLabel:({cxo:'CEO / CXO',director:'Director',manager:'Manager'})[x[2]]||''})),'dsg_');
  /* counts_approval_requests kinds; shifting approved by Park Head, birth/death by named counts approvers */
  st.approvalChains=mk([['Counts','Birth',['counts_approver']],['Counts','Death',['counts_approver']],['Counts','Shifting',['park_head']]].map(x=>({dept:x[0],name:x[1],steps:x[2].map(d=>d==='counts_approver'?{role:d,perPerson:true}:{designation:d})})),'apc_');
};
