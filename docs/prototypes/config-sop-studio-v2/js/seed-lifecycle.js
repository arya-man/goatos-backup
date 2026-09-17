/* Lifecycle seed: pen-tag stages, health states, groups. Swappable data file.
   DAY RANGES: values from goatos-stg animal_stage_lookup (tenant 00000000-0000-4000-8000-000000000001),
   queried 2026-09-18 read-only. This SUPERSEDES the Drive doc "Goats and Parks" (PARK SHED TAGS),
   which had K0 1-2, K1 3-9, K2 10-77, K3 78-84, Fattening 85-240 and adults 300+.
   Authoritative stg rows (stage_code | min_age_days | max_age_days):
     K0 0|1, K1 2|7, K2 8|42, K3 43|NULL, F2 NULL|NULL, F2-Male NULL|NULL, F2-Female NULL|NULL,
     Buck/Mother/Milking/M0/Warmup/Pregnant/Non-Pregnant/Flushing/ICU/ICU-Kid/Quarantine/Quarantine kids all NULL|NULL.
   stg defines age bounds for K0-K3 only; every other stage is unbounded there, so those rows carry
   no day range here. Stage names/codes stay as the app uses them; stg is source of truth for days only. */
(function(){
  const T=[
    ['K0','K0 - Newborn',0,1,'any'],['K1','K1 - Milk Training',2,7,'any'],['K2','K2 - Milk Drinking',8,42,'any'],['K3','K3 - Weaning',43,'','any'],
    ['FAT-M','Fattening Male','','','male',['F2-Male']],['FAT-F','Fattening Female','','','female',['F2-Female']],
    ['FATW-M','Fattening Male Warmup','','','male'],['FATW-F','Fattening Female Warmup','','','female'],
    ['WU-NP','Warmup Non Pregnant','','','female'],['WU-P','Warmup Pregnant','','','female'],['WU-B','Warmup Buck','','','male'],
    ['NP','Non Pregnant','','','female',['Non-Pregnant']],['FLUSH','Flushing','','','female'],['BREED','Breeding','','','any'],
    ['PEG','Pregnant Early Gestation','','','female',['Pregnant']],['PLG','Pregnant Late Gestation','','','female'],
    ['MOTHER','Mother','','','female'],['MMW','Mother Milking Waiting','','','female'],['MWU','Milking Warmup','','','female'],
    ['MILK','Milking','','','female'],['BUCK','Buck','','','male']];
  const stages=[];
  [['sp_goat','g'],['sp_sheep','s']].forEach(([sp,p])=>T.forEach(t=>stages.push({id:'stg_'+p+'_'+t[0].toLowerCase().replace(/-/g,''),speciesId:sp,code:t[0],name:t[1],fromD:t[2],toD:t[3],sex:t[4],aliases:t[5]||[]})));
  window.LIFECYCLE_SEED={
    stages,
    /* Breeding is a pen-tag stage only (G&P). F2 / "F2 - Fattening" are legacy names for plain Fattening; sex comes from the animal.
       stg carries F2, F2-Male and F2-Female as stage rows, all with no age bounds. */
    tags:[{name:'Fattening',kind:'group',aliases:['F2','F2 - Fattening']}],
    /* stg ICU-Kid / Quarantine kids / ICU / Quarantine have NULL min/max age, so no day range here.
       stg has no Fattening-Kids ICU/Quarantine row; kept as an app-side grouping with no bounds. */
    healthStates:[
      {name:'ICU Milk Kids',group:'Milk Kids',fromD:'',toD:'',aliases:['ICU-Kid']},{name:'Quarantine Milk Kids',group:'Milk Kids',fromD:'',toD:'',aliases:['Quarantine kids']},
      {name:'ICU Fattening Kids',group:'Fattening Kids',fromD:'',toD:''},{name:'Quarantine Fattening Kids',group:'Fattening Kids',fromD:'',toD:''},
      {name:'ICU Adults',group:'Adults',fromD:'',toD:'',aliases:['ICU']},{name:'Quarantine Adults',group:'Adults',fromD:'',toD:'',aliases:['Quarantine']}]
  };
})();
