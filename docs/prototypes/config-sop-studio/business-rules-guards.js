/* Keep older setting entry points and entity maintenance on the shared rule contract. */
const businessRuleScalarFields={reporting_weight_35:'weightKg',valuation_fattening_rate:'rate'};
const businessRulesLegacyItemModal=itemModal;
itemModal=function(id){const item=id?itemById(id):null;if(item&&businessRuleScalarFields[item.configKey])return businessRuleEdit('global');return businessRulesLegacyItemModal(id);};
const businessRulesLegacyCommitItem=commitItem;
commitItem=function(item){
 const field=item&&businessRuleScalarFields[item.configKey];if(!field)return businessRulesLegacyCommitItem(item);
 if(!canEdit())return;
 try{
  const old=itemById(item.id);if(!old||old.configKey!==item.configKey)throw Error('Edit the existing default business rule.');
  if(item.active===false||item.unit!==old.unit)throw Error('Default business rules must remain active with their configured units.');
  const typedError=validateTypedConfig(item,old);if(typedError)throw Error(typedError);
  const next=structuredClone(businessRulesData());next.global[field]=item.value;next.version++;
  const errors=BusinessRulesModel.validate(next,entityRegistryData());if(errors.length)throw Error(errors.join(' '));
  const lower=configValueByKey('reporting_weight_30');if(lower&&next.global.weightKg<=lower.value)throw Error('Upper reporting weight must exceed the lower reporting weight.');
  businessRulesPersist(next);closeModal();render();toast('Business rules saved in this browser');
 }catch(e){const error=document.querySelector('#itemerror')||document.querySelector('#br-error');if(error)error.textContent=e.message;else toast(e.message);}
};
function businessRulesEntityReferences(kind,id){
 const field={species:'speciesId',breeds:'breedId',sex_values:'sexId'}[kind];
 const model=state.businessRules||(field&&state.salesPolicyPreview?businessRulesData():null);
 return field?(model?.overrides||[]).filter(r=>r.active!==false&&r[field]===id):[];
}
const businessRulesEntityArchive=EntityRegistryModel.archive;
EntityRegistryModel.archive=function(data,kind,id){
 if(businessRulesEntityReferences(kind,id).length)throw Error('Archive or update the linked business rules before archiving this record.');
 return businessRulesEntityArchive.call(this,data,kind,id);
};
const businessRulesEntitySave=EntityRegistryModel.save;
EntityRegistryModel.save=function(data,kind,input){
 const old=this.rows(data,kind).find(r=>r.id===input.id);
 if(old&&input.active===false&&businessRulesEntityReferences(kind,old.id).length)throw Error('Archive or update the linked business rules before archiving this record.');
 if(old&&kind==='breeds'&&old.parentId!==input.parentId&&businessRulesEntityReferences(kind,old.id).length)throw Error('Update the linked business rules before changing this breed’s animal type.');
 return businessRulesEntitySave.call(this,data,kind,input);
};

// Retired Sales publishing form cannot write a second copy of the same defaults.
saveSales=function(){if(!canEdit())return;businessRuleEdit('global');};
