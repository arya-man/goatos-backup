/* Source-aware local configuration. Production stores and identities are not migrated. */
const typedConfigTypes=['number','integer','currency','weight','quantity','percent','time','boolean','enum','text'];
const typedConfigSourceLabels={observed_config:'Existing setting · practice copy',hardcoded_source:'Currently fixed in the app · proposed setting',proposed_config:'New setting · practice only',derived_reference:'View only · managed in its department',fallback:'Default value'};
function ensureTypedConfig(){
 ensureItemState();if(state.typedConfigSeeded)return;
 const cat='category-shared-configuration';if(!itemCategory(cat))state.itemCategories.push({id:cat,name:'Configuration values',module:'Common',parentId:null,shares:[]});
 const sub='subcategory-shared-settings';if(!itemCategory(sub))state.itemCategories.push({id:sub,name:'Operational settings',module:'Common',parentId:cat,shares:[]});
 const records=[
 ['reporting_weight_30','Lower reporting weight','weight',30,'kg',['Weighing','Sales'],'hardcoded_source','Weighing','backend/internal/weighing/domain/shed_weights.go:260','Reporting threshold; not a sale prohibition'],
 ['reporting_weight_35','Upper reporting weight','weight',35,'kg',['Weighing','Sales'],'hardcoded_source','Weighing','backend/internal/weighing/domain/shed_weights.go:261','Reporting threshold; not a sale prohibition'],
 ['valuation_fattening_rate','Fattening valuation rate','currency',450,'INR/kg',['Sales'],'hardcoded_source','Sales','backend/internal/sales/adapters/postgres/overview_repository.go:617','Fattening valuation assumption; not a recorded sale price'],
 ['removal_cutoff','Feed and water removal time','time','21:00','IST',['Feed','Weighing','Preventive Care'],'observed_config','Feed','feed_water_removal_config.cutoff_time','Farm-wide cutoff'],
 ['market_call_time','Market call time','time','08:00','IST',['Sales','Procurement'],'observed_config','Sales','market survey call_time','Daily market survey'],
 ['weighing_daily_cap','Weighing daily capacity','integer',100,'animals/day',['Weighing'],'observed_config','Weighing','weighing.session v1 planning.default_cap_per_day','Default capacity for new plans'],
 ['lump_video_min','Lump-sum minimum videos','integer',1,'videos',['Weighing'],'observed_config','Weighing','weighing.session v1 capture.lump_sum.video_min','Published proof policy copy'],
 ['lump_video_max','Lump-sum maximum videos','integer',5,'videos',['Weighing'],'observed_config','Weighing','weighing.session v1 capture.lump_sum.video_max','Published proof policy copy'],
 ['verification_feed_percent','Feed verification sample','percent',75,'percent',['Feed'],'observed_config','Verification','verification sampling feed_distribution effective 2026-09-11','Effective from 2026-09-11'],
 ['vaccination_capacity','Vaccination daily capacity','integer',200,'animals/day',['Preventive Care'],'observed_config','Preventive Care','vaccination.matrix v9 capacity.max_per_day','Tenant capacity; not a dose'],
 ['feed_ration_sample','Dry Masoor Bhusa ration','quantity',1900,'g/head',['Feed'],'observed_config','Feed','feed ration read-only snapshot 2026-09-16','Boer / Buck / Dry Masoor Bhusa; park and effective-date specific example'],
 ['individual_video_required','Individual weighing video required','boolean',true,'boolean',['Weighing'],'derived_reference','Weighing','weighing.session v1 capture.individual.video_required','Locked required invariant'],
 ['removal_mode','Weighing removal mode','enum','required','enum',['Weighing'],'observed_config','Weighing','weighing.session v1 feed_water_removal.mode','New weighing plans'],
 ['health_protocol_reference','Health treatment protocol','text','Published disease × age-band versions','text',['Health'],'derived_reference','Health','health_protocol_versions','Medication, action and critical action steps; edit in the existing versioned Health Config'],
 ];
 records.forEach(([key,name,type,value,unit,shares,sourceKind,sourceOwner,sourceRef,scope])=>{if(state.items.some(i=>i.configKey===key))return;state.items.push({id:'config-'+key,name,module:'Common',categoryId:cat,subcategoryId:sub,purpose:scope,description:'Local reference from the architecture and staging research. Changes here do not update the source system.',unit,active:true,shares,revision:1,itemKind:'config_value',configKey:key,valueType:type,value,currency:type==='currency'?'INR':'',scope,sourceKind,sourceOwner,sourceRef,sourceValue:value,enumOptions:key==='removal_mode'?['required','optional','off']:[]});});
 state.typedConfigSeeded=true;
}
function configValueByKey(key){ensureTypedConfig();return state.items.find(i=>i.itemKind==='config_value'&&i.configKey===key&&i.active)||null;}
function configNumber(key,fallback){const i=configValueByKey(key);return i&&typeof i.value==='number'&&Number.isFinite(i.value)?i.value:fallback;}
function configDisplayValue(item){if(item?.value===null||item?.value===undefined||item?.value==='')return 'Not configured';return typeof item.value==='boolean'?(item.value?'Yes':'No'):String(item.value)+(item.unit&&!['boolean','enum','text'].includes(item.unit)?' '+item.unit:'');}
function typedConfigValue(type,raw){if(raw===null||raw===undefined||raw==='')return null;if(['number','integer','currency','weight','quantity','percent'].includes(type)){const n=Number(raw);return Number.isFinite(n)?n:NaN;}if(type==='boolean')return raw===true||raw==='true'?true:raw===false||raw==='false'?false:String(raw);return String(raw).trim();}
function validateTypedConfig(item,existing=null){
 if(existing&&existing.itemKind!=='config_value'&&item.itemKind==='config_value')return 'Saved catalogue items retain their item type. Create a new configuration value instead.';
 if(existing?.itemKind==='config_value'&&item.itemKind!=='config_value')return 'This setting is used in work instructions. Keep it as a setting.';
 if(item.itemKind!=='config_value')return '';
 if(existing?.itemKind==='config_value'&&(item.valueType!==existing.valueType||item.unit!==existing.unit||item.currency!==existing.currency))return 'This setting is already in use. Keep its value type, unit and currency; create a separate setting for another unit.';
 if(!/^[a-z][a-z0-9_]{1,79}$/.test(item.configKey||''))return 'Use a stable key with lowercase letters, numbers and underscores.';
 if(!typedConfigTypes.includes(item.valueType))return 'Choose a supported value type.';
 const type=item.valueType,v=item.value;if(v===null||v===undefined||v==='')return 'Enter a value. Zero is a valid value; a blank is not zero.';
 if(['number','integer','currency','weight','quantity','percent'].includes(type)&&(!Number.isFinite(v)||typeof v!=='number'))return 'Enter a finite numeric value.';
 if(type==='integer'&&!Number.isInteger(v))return 'Enter a whole number.';
 if(['currency','weight','quantity','integer'].includes(type)&&v<0)return 'Enter zero or a positive value.';
 if(type==='quantity'&&!String(item.unit||'').trim())return 'Quantity requires a unit, for example g/head or kg.';
 if(['lump_video_min','lump_video_max'].includes(item.configKey)){const other=configValueByKey(item.configKey==='lump_video_min'?'lump_video_max':'lump_video_min');if(v<1)return 'This weighing proof count must be at least one.';if(!other||typeof other.value!=='number')return 'Restore the paired weighing video configuration before editing this bound.';if(typeof other.value==='number'&&(item.configKey==='lump_video_min'?v>other.value:v<other.value))return 'Minimum videos must not exceed maximum videos in this weighing policy.';}
 if(type==='percent'&&(v<0||v>100))return 'Percentage must be between 0 and 100.';
 if(type==='time'&&!/^([01]\d|2[0-3]):[0-5]\d$/.test(v))return 'Enter a valid time as HH:MM.';
 if(type==='weight'&&!['kg','g'].includes(item.unit))return 'Weight requires kg or g.';
 if(type==='percent'&&item.unit!=='percent')return 'Percentage requires percent units.';
 if(type==='time'&&item.unit!=='IST')return 'Time uses the IST business clock.';
 if(type==='boolean'&&(typeof v!=='boolean'||item.unit!=='boolean'))return 'Boolean values require Yes/No and boolean units.';
 if(type==='currency'&&(!/^[A-Z]{3}$/.test(item.currency||'')||(!item.unit.startsWith(item.currency+'/')||!item.unit.slice(item.currency.length+1).trim())))return 'Price requires currency and a matching per-unit rate, for example INR/kg.';
 if(type==='enum'&&!(item.enumOptions||[]).includes(v))return 'Choose one of the configured options.';
 if(!item.scope?.trim()||!item.sourceOwner?.trim())return 'Enter the scope and source owner.';
 return '';
}
const typedBaseRenderItems=renderItems;
renderItems=function(){ensureTypedConfig();typedBaseRenderItems();const title=$('.items-heading h1');if(title)title.textContent='Items and settings';};
const typedBaseRows=renderItemRows;
renderItemRows=function(){typedBaseRows();document.querySelectorAll('#itemrows tr').forEach(row=>{const button=row.querySelector('.item-name');if(!button)return;const i=state.items.find(x=>x.name===button.textContent);if(i?.itemKind==='config_value')button.insertAdjacentHTML('afterend',`<div class="typed-value-summary">${esc(configDisplayValue(i))} <span class="muted">· ${esc(typedConfigSourceLabels[i.sourceKind]||i.sourceKind)}</span></div>`);});};
const typedBaseModal=itemModal;
itemModal=function(id){ensureTypedConfig();typedBaseModal(id);const form=$('#itemform');if(!form)return;const i=id?itemById(id):null,readOnly=i?.sourceKind==='derived_reference',dis=!canEdit()||readOnly?'disabled':'';
 const unit=$('#iunit');if(i?.unit&&!Array.from(unit.options).some(o=>o.value===i.unit))unit.insertAdjacentHTML('beforeend',`<option selected value="${esc(i.unit)}">${esc(i.unit)}</option>`);
 form.insertAdjacentHTML('afterbegin',`<label class="field"><span>Item type</span><select id="itypekind" onchange="typedToggleFields()" ${i?.id?'disabled':dis}><option value="catalogue" ${i?.itemKind!=='config_value'?'selected':''}>Item — something we use</option><option value="config_value" ${i?.itemKind==='config_value'?'selected':''}>Setting — a value or rule</option></select>${i?.id?'<small>To change from an item to a setting, create a new entry. This keeps existing work instructions working.</small>':''}</label><section id="typed-config-fields"><div class="inputrow"><label class="field"><span>What kind of value?</span><select id="ivalueType" onchange="typedChangeType()" ${i?.configKey?'disabled':dis}>${typedConfigTypes.map(t=>`<option value="${t}" ${i?.valueType===t?'selected':''}>${({number:"Number",integer:"Whole number",currency:"Money / price",weight:"Weight",quantity:"Amount / quantity",percent:"Percentage",time:"Time of day",boolean:"Yes or no",enum:"Choose from a list",text:"Text"})[t]}</option>`).join('')}</select></label><label class="field"><span>Configuration key</span><input id="iconfigKey" value="${esc(i?.configKey||'')}" placeholder="purchase_rate_per_kg" ${i?.configKey?'readonly':''} ${dis}></label></div><label class="field"><span>Value</span><input id="iconfigValue" value="${esc(i?.value===null||i?.value===undefined?'':String(i.value))}" ${dis}></label><div class="inputrow"><label class="field"><span>Unit</span><input id="iconfigUnit" ${i?.configKey?'readonly':''} value="${esc(i?.itemKind==='config_value'?i.unit:'unit')}" placeholder="INR/kg, kg, g/head, percent, IST" ${dis}></label><label class="field"><span>Currency</span><input id="iconfigCurrency" ${i?.configKey?'readonly':''} value="${esc(i?.currency||'INR')}" maxlength="3" ${dis}></label></div><label class="field"><span>Choices (separate with commas)</span><input id="iconfigEnum" oninput="typedRenderValueControl()" value="${esc((i?.enumOptions||[]).join(', '))}" ${dis}></label><label class="field"><span>Where is this intended to apply? (note only)</span><input id="iconfigScope" value="${esc(i?.scope||'')}" placeholder="Describe intended tenant, park, cohort or dates" ${dis}></label><label class="field"><span>Responsible department</span><input id="iconfigOwner" value="${esc(i?.sourceOwner||'')}" placeholder="Feed, Weighing, Sales…" ${dis}></label><div class="notice">${esc(typedConfigSourceLabels[i?.sourceKind]||typedConfigSourceLabels.proposed_config)}${i?.sourceValue!==undefined?'<br>Observed/source value: '+esc(String(i.sourceValue)):''}<br>This note describes your intention; it does not restrict the setting to a park, group or date. Changes here are for practice and do not change farm records or instructions already in use.</div></section>`);
 const nameRow=$('#iname').closest('.inputrow')||$('#iname').closest('label');if(nameRow)form.prepend(nameRow);
 if(readOnly){form.querySelectorAll('input,select,textarea,button[type="submit"]').forEach(e=>e.disabled=true);form.querySelectorAll('.generic-category-create button').forEach(e=>e.disabled=true);}
 const details=document.createElement('details');details.className='typed-source-details';details.innerHTML='<summary>Technical reference — for implementation</summary>';
 const keyLabel=$('#iconfigKey').closest('label');details.appendChild(keyLabel);details.insertAdjacentHTML('beforeend','<p class="muted">'+esc(i?.sourceRef||'New local configuration; its key is generated from the item name.')+'</p>');$('#typed-config-fields').appendChild(details);
 typedToggleFields();typedRenderValueControl();
};
function typedToggleFields(){const section=$('#typed-config-fields');if(section)section.hidden=$('#itypekind').value!=='config_value';const originalUnit=$('#iunit')?.closest('label');if(originalUnit)originalUnit.hidden=$('#itypekind').value==='config_value';const type=$('#ivalueType')?.value;if($('#iconfigCurrency'))$('#iconfigCurrency').closest('label').hidden=type!=='currency';if($('#iconfigEnum'))$('#iconfigEnum').closest('label').hidden=type!=='enum';}
function typedChangeType(){const type=$('#ivalueType').value,units={weight:'kg',percent:'percent',time:'IST',boolean:'boolean',enum:'enum',text:'text',currency:'INR/kg',integer:'count',number:'unit',quantity:'g/head'};$('#iconfigUnit').value=units[type];typedToggleFields();typedRenderValueControl();}
function typedReadFields(id){const old=id?itemById(id):null,kind=$('#itypekind').value;if(kind==='catalogue')return {itemKind:kind};const type=$('#ivalueType').value;return {itemKind:kind,configKey:$('#iconfigKey').value.trim()||typedKeyFromName($('#iname').value),valueType:type,value:typedConfigValue(type,$('#iconfigValue').value.trim()),unit:$('#iconfigUnit').value.trim(),currency:type==='currency'?$('#iconfigCurrency').value.trim().toUpperCase():'',scope:$('#iconfigScope').value.trim(),sourceOwner:$('#iconfigOwner').value.trim(),sourceKind:old?.sourceKind||'proposed_config',sourceRef:old?.sourceRef||'Created in local mock',sourceValue:old?.sourceValue,enumOptions:$('#iconfigEnum').value.split(',').map(s=>s.trim()).filter(Boolean)};}
let typedPendingMetadata=null;
const typedBaseSave=saveItem;
saveItem=function(id){
 if(!canEdit())return;if(id&&itemById(id)?.sourceKind==='derived_reference')return;
 if(!$('#itypekind'))return typedBaseSave(id);
 const metadata=typedReadFields(id),error=validateTypedConfig(metadata,id?itemById(id):null);
 if(error){$('#itemerror').textContent=error;return;}
 if(metadata.configKey&&state.items.some(i=>i.id!==id&&i.configKey===metadata.configKey)){$('#itemerror').textContent='This configuration key already exists.';return;}
 if(metadata.itemKind==='config_value'){const unit=$('#iunit');if(!Array.from(unit.options).some(o=>o.value===metadata.unit))unit.insertAdjacentHTML('beforeend',`<option value="${esc(metadata.unit)}">${esc(metadata.unit)}</option>`);unit.value=metadata.unit;}
 typedPendingMetadata=metadata;try{typedBaseSave(id);if(window.pendingItemSave?.id===id)Object.assign(window.pendingItemSave,metadata);}finally{typedPendingMetadata=null;}
};
const typedBaseCommit=commitItem;
commitItem=function(item){const old=itemById(item.id);if(old?.sourceKind==='derived_reference')return;const next={...old,...item,...(typedPendingMetadata||{})};if(next.itemKind==='config_value'&&old&&next.value!==old.value)next.locallyModified=true;typedBaseCommit(next);};

function typedKeyFromName(name){const key=String(name).trim().toLowerCase().replace(/[^a-z0-9]+/g,'_').replace(/^_+|_+$/g,'').slice(0,70);return /^[a-z]/.test(key)?key:'value_'+key;}
function typedRenderValueControl(){
 const control=$('#iconfigValue');if(!control)return;const type=$('#ivalueType').value,value=control.value,dis=control.disabled?'disabled':'';
 if(type==='boolean'){control.outerHTML=`<select id="iconfigValue" ${dis}><option value="">Choose…</option><option value="true" ${value==='true'?'selected':''}>Yes</option><option value="false" ${value==='false'?'selected':''}>No</option></select>`;return;}
 if(type==='enum'){const options=$('#iconfigEnum').value.split(',').map(s=>s.trim()).filter(Boolean);control.outerHTML=`<select id="iconfigValue" ${dis}><option value="">Choose…</option>${options.map(o=>`<option value="${esc(o)}" ${value===o?'selected':''}>${esc(o)}</option>`).join('')}</select>`;return;}
 control.outerHTML=`<input id="iconfigValue" type="${type==='time'?'time':['number','integer','currency','weight','quantity','percent'].includes(type)?'number':'text'}" ${type==='integer'?'step="1"':'step="any"'} value="${esc(value)}" ${dis}>`;
}
// Inventory/action pickers must not offer configuration values as physical items.
const typedBaseAvailableItems=availableItems;
availableItems=function(module){return typedBaseAvailableItems(module).filter(i=>i.itemKind!=='config_value');};
const typedBaseSourceOptions=sourceOptions;
sourceOptions=function(source,module=current){return typedBaseSourceOptions(source,module).filter(i=>i.itemKind!=='config_value');};
function configValuesForModule(module){ensureTypedConfig();return state.items.filter(i=>i.itemKind==='config_value'&&i.active&&genericItemAccess(i).some(a=>a.module===module));}
// Numeric branch dependencies use the same impact-review path as catalogue references.
const typedBaseItemUsages=itemUsages;
itemUsages=function(id){
 const usages=typedBaseItemUsages(id),item=itemById(id);if(item?.itemKind!=='config_value')return usages;
 function inspectVersion(s,module,kind,activeDraft=false){if(!s)return;const pinned=!!s.definition;for(const n of (s.definition?.nodes||s.nodes||[])){
  if(n.type!=='condition')continue;
  const uses=[n,...(n.clauses||[])].some(c=>pinned&&c.configSnapshot?.id?c.configSnapshot.id===id:c.valueRef==='config:'+id||(!String(c.valueRef||'').startsWith('config:')&&typeof qrConfigKey==='function'&&qrConfigKey(c.valueRef)===item.configKey));
  if(uses)usages.push({title:s.title,module,kind,nodeId:n.id,nodeLabel:n.label||'Numeric comparison',nodeType:'condition',activeDraft});
 }}
 function workflow(s,module,active){inspectVersion(s,module,active?'Draft':'Workflow draft',active);inspectVersion(s.published,module,'Published v'+s.version);(s.versions||[]).forEach(v=>inspectVersion(v,module,'Published v'+v.version));}
 Object.entries(state.sops||{}).forEach(([m,s])=>workflow(s,m,true));Object.entries(state.archivedSops||{}).forEach(([m,list])=>list.forEach(s=>workflow(s,m,false)));
 return usages;
};
