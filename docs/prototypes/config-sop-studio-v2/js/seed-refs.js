/* Reference lists seed (from staging catalogues: sop_categories, sop_task_types, status_definitions, goats exit CHECKs, sold weight bands) */
window.seedRefs=function(st){
  const mk=(arr,f)=>arr.map((x,i)=>Object.assign({id:f+i,status:'active'},x));
  st.sopCategories=mk([['commodity','Commodity'],['problem','Problem'],['event','Event'],['action','Action'],['equipment','Equipment / Asset']].map(x=>({code:x[0],name:x[1]})),'sc_');
  const tt=['administer','weigh','tag','inspect','verify','transfer','clean','schedule_next','photo_record','calculate','death_evidence','record_pen','record_yes_no','do_and_confirm','record_select','video_record','feed_colostrum','record_text','record_number','record_multiselect','return_to_pen'];
  const ans={record_yes_no:'yes_no',record_select:'select',record_multiselect:'multiselect',record_number:'number',weigh:'number',record_text:'text'};
  st.taskTypes=mk(tt.map(c=>({code:c,name:c.replace(/_/g,' ').replace(/^./,m=>m.toUpperCase()),answer:ans[c]||'none'})),'tt_');
  const sd=[['lifecycle',['alive','dead','sold','merged','inactive']],['reproductive',['pregnant','non_pregnant','mother','milking','buck']],['health',['healthy','sick','under_treatment','recovering','icu','quarantine']]];
  st.statusDefs=[];sd.forEach(([ax,codes])=>codes.forEach(c=>st.statusDefs.push({id:'sd_'+ax+'_'+c,axis:ax,code:c,name:c.replace(/_/g,' ').replace(/^./,m=>m.toUpperCase()),days:'',status:'active'})));
  st.exitReasons=mk(['Sold','Died','Culled','Transferred','Lost'].map(n=>({name:n})),'ex_');
  st.purposes=mk(['Breeding','Fattening'].map(n=>({name:n})),'pu_');
  st.movementReasons=mk(['Growth','Delivery','Breeding','Health'].map(n=>({name:n})),'mv_');
  st.weightBands=mk([['Under 20 kg','',20],['20–35 kg',20,35],['35–40 kg',35,40],['40 kg +',40,'']].map(x=>({speciesId:'',name:x[0],fromKg:x[1],toKg:x[2]})),'wb_');
  st.symptoms=mk([['FEVER','Fever'],['HIGH_FEVER','High fever'],['HYPOTHERMIA','Hypothermia'],['TENT_GT4','Skin tent over 4 s']].map(x=>({code:x[0],name:x[1]})),'sy_');
  st.diseases=mk(['Bloat','Acidosis','Diarrhea','Fever','Heat stress','Mastitis','Anemia','PPR','Pox','ORF','Foot rot','Pinkeye'].map(n=>({name:n,ageBand:''})),'di_');
  st.deathCauses=st.deathCauses||[];st.marketCities=st.marketCities||[];st.rationGroups=st.rationGroups||[];
};
