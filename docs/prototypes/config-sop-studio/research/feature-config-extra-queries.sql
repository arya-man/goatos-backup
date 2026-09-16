SELECT 'inventory' AS section,item_code,name,category,base_unit,status FROM inventory_items ORDER BY item_code;
SELECT 'location_flags' AS section,usable_for_counts,usable_for_feed,usable_for_vaccination,usable_for_sop,is_holding,is_quarantine,is_icu,count(*) FROM location_operational_attributes GROUP BY 2,3,4,5,6,7,8;
SELECT 'vaccination_operators' AS section,active_operators_per_day,cardinality(selected_operator_ids) AS selected_count,count(*) FROM vaccination_operator_assignment_config GROUP BY 2,3;
SELECT 'vaccination_shifts' AS section,shift_start_minute,shift_end_minute,week_off_weekday,count(*) FROM vaccination_operator_shift_config GROUP BY 2,3,4;
SELECT 'roles' AS section,tier_code,vertical_code,is_legacy,count(*) FROM org_role_catalog GROUP BY 2,3,4 ORDER BY 2,3;
SELECT 'vendor_nonpersonal_catalog' AS section,kind,value,label,register_side,is_active FROM procurement_vendor_catalog WHERE kind IN ('breed','capacity_unit','feed','record_type','status','supply_frequency') ORDER BY kind,register_side,value;
SELECT 'ui_copy' AS section,route_id,config_key,config_value,value_kind,status FROM admin_ui_config_entries;
SELECT 'clinical_categories' AS section,category,status,count(*) FROM protocol_definitions GROUP BY 2,3;
SELECT 'published_sop_config' AS section,d.code,v.version,v.form_dsl,v.proof_policy,v.compatibility FROM sop_definitions d JOIN sop_versions v USING(sop_id) WHERE v.status='published' AND d.code NOT IN ('procurement.animal_purchase','milk.feeding','milk.preparation') ORDER BY d.code;
