/* Lifecycle seed: pen-tag stages, health states, groups. Source: Goats and Parks (PARK SHED TAGS). Swappable data file. */
(function(){
  const T=[
    ['K0','K0 - Newborn',1,2,'any'],['K1','K1 - Milk Training',3,9,'any'],['K2','K2 - Milk Drinking',10,77,'any'],['K3','K3 - Weaning',78,84,'any'],
    ['FAT-M','Fattening Male',85,240,'male',['F2-Male']],['FAT-F','Fattening Female',85,240,'female',['F2-Female']],
    ['FATW-M','Fattening Male Warmup',120,240,'male'],['FATW-F','Fattening Female Warmup',120,240,'female'],
    ['WU-NP','Warmup Non Pregnant',300,'','female'],['WU-P','Warmup Pregnant',300,'','female'],['WU-B','Warmup Buck',300,'','male'],
    ['NP','Non Pregnant',300,'','female',['Non-Pregnant']],['FLUSH','Flushing',300,'','female'],['BREED','Breeding',300,'','any'],
    ['PEG','Pregnant Early Gestation',300,'','female',['Pregnant']],['PLG','Pregnant Late Gestation',300,'','female'],
    ['MOTHER','Mother',300,'','female'],['MMW','Mother Milking Waiting',300,'','female'],['MWU','Milking Warmup',300,'','female'],
    ['MILK','Milking',300,'','female'],['BUCK','Buck',300,'','male']];
  const stages=[];
  [['sp_goat','g'],['sp_sheep','s']].forEach(([sp,p])=>T.forEach(t=>stages.push({id:'stg_'+p+'_'+t[0].toLowerCase().replace(/-/g,''),speciesId:sp,code:t[0],name:t[1],fromD:t[2],toD:t[3],sex:t[4],aliases:t[5]||[]})));
  window.LIFECYCLE_SEED={
    stages,
    /* Breeding is a pen-tag stage only (G&P). F2 / "F2 - Fattening" are legacy names for plain Fattening; sex comes from the animal. */
    tags:[{name:'Fattening',kind:'group',aliases:['F2','F2 - Fattening']}],
    healthStates:[
      {name:'ICU Milk Kids',group:'Milk Kids',fromD:3,toD:77,aliases:['ICU-Kid']},{name:'Quarantine Milk Kids',group:'Milk Kids',fromD:3,toD:77,aliases:['Quarantine kids']},
      {name:'ICU Fattening Kids',group:'Fattening Kids',fromD:85,toD:240},{name:'Quarantine Fattening Kids',group:'Fattening Kids',fromD:85,toD:240},
      {name:'ICU Adults',group:'Adults',fromD:300,toD:'',aliases:['ICU']},{name:'Quarantine Adults',group:'Adults',fromD:300,toD:'',aliases:['Quarantine']}]
  };
})();
