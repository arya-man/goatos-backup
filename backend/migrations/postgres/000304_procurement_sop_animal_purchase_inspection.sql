-- +goose Up
-- 000304_procurement_sop_animal_purchase_inspection.sql
--
-- PROCUREMENT SOP (maintainer decision 2026-09-14, docs/decisions/procurement-sop.md).
-- The per-animal purchase inspection the phone runs -- which questions, on which page, which
-- take a photo / a video / either, which are compulsory -- moves from a Go catalog
-- (animalpurchase/domain.Questionnaire) into the SOP library: the `inspection` section of the
-- PUBLISHED procurement.animal_purchase version, authored on /procurement/sops.
--
-- This publishes VERSION 1 = exactly the catalog the code served until now (pinned by
-- TestSeededInspectionCompilesToTheLegacyQuestionnaire and TestMigrationEmbedsTheSeededInspection:
-- the document below is the embedded seed byte for byte). Rows already recorded carry
-- questionnaire_version = 1 and keep reading this version. Idempotent per tenant.
--
-- seed-fixture-guard:ignore: SOP library document seed (sop_definitions/sop_versions rows only);
-- no vaccination / HRMS / goats schema moves.
-- seed-migration-guard:ignore owner=claude issue=procurement-sop reason=library-document seed for an existing phone flow; idempotent definition + version insert, no read-model or clean-slate change expiry=2026-10-31

INSERT INTO public.sop_definitions (tenant_id, code, name, description, status)
SELECT t.tenant_id, 'procurement.animal_purchase', 'Animal Purchase Inspection',
       'The per-animal inspection the procurement desk records inside a purchase load: pages of questions, the photos and videos each needs, and the inspector''s own verdict. The CEO/CXO accepts or rejects each animal on the web.',
       'active'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Animal Purchase Inspection v1', 'published',
jsonb_build_object(
  'schema_version', 'goatos.sop-form.v1',
  'sop_code', 'procurement.animal_purchase',
  'title', 'Animal Purchase Inspection',
  'fields', '[
    {"key": "vendor", "type": "text", "label": "Vendor", "required": true, "description": "From the vendor register; lives on the load, not on the animal."},
    {"key": "farm", "type": "select", "label": "Farm", "options": ["CBE", "CPT"], "required": true},
    {"key": "load_ref", "type": "text", "label": "Load number", "required": true},
    {"key": "expected_count", "type": "number", "label": "Roughly how many animals", "required": false, "min": 0},
    {"key": "notes", "type": "text", "label": "Note", "required": false},
    {"key": "animal_video", "type": "video_proof", "label": "Animal video", "required": true, "proof_action": "video.capture", "description": "Captured per animal inside the inspection pages below."},
    {"key": "animal_photo", "type": "photo_proof", "label": "Animal photo", "required": true, "proof_action": "photo.capture", "description": "Captured per animal inside the inspection pages below."}
  ]'::jsonb,
  'rules', '[]'::jsonb,
  'inspection', $seed${
  "pages": [
    {
      "key": "identity",
      "questions": [
        {
          "id": "species",
          "kind": "choice",
          "title": "Goat or sheep",
          "required": true,
          "options": [
            {
              "value": "goat",
              "label": "Goat"
            },
            {
              "value": "sheep",
              "label": "Sheep"
            }
          ]
        },
        {
          "id": "goat_id",
          "kind": "text",
          "title": "Goat ID",
          "hint": "The tag or number the vendor uses for this animal.",
          "required": true
        },
        {
          "id": "well_fed",
          "kind": "choice",
          "title": "Is the animal well fed and walking actively?",
          "hint": "REJECT if the animal is visibly empty or walking weakly. Yes if well fed, No if ribs are visible.",
          "required": true,
          "options": [
            {
              "value": "yes",
              "label": "Yes"
            },
            {
              "value": "no",
              "label": "No"
            }
          ]
        },
        {
          "id": "teeth",
          "kind": "number",
          "title": "How many fully and half formed teeth?",
          "hint": "If its mouth can't be opened, REJECT IMMEDIATELY.",
          "required": true,
          "min": 0,
          "max": 8
        },
        {
          "id": "teeth_media",
          "kind": "media",
          "title": "Photo of teeth",
          "required": true,
          "slot": "teeth",
          "max_files": 5,
          "accepts": [
            "photo",
            "video"
          ]
        },
        {
          "id": "sex",
          "kind": "choice",
          "title": "Gender of the animal?",
          "required": true,
          "options": [
            {
              "value": "female",
              "label": "Female"
            },
            {
              "value": "male",
              "label": "Male"
            }
          ]
        },
        {
          "id": "pregnant",
          "kind": "choice",
          "title": "Is the animal pregnant?",
          "required": true,
          "options": [
            {
              "value": "no",
              "label": "No"
            },
            {
              "value": "yes",
              "label": "Yes"
            }
          ],
          "only_if": {
            "question_id": "sex",
            "value": "female"
          }
        },
        {
          "id": "weight_kg",
          "kind": "number",
          "title": "Weight of the animal in KG",
          "required": false,
          "min": 0.5,
          "max": 300,
          "unit": "kg"
        },
        {
          "id": "weight_media",
          "kind": "media",
          "title": "Weight of the animal media",
          "hint": "The scale reading with the animal on it.",
          "required": false,
          "slot": "weight",
          "max_files": 1,
          "accepts": [
            "photo",
            "video"
          ]
        },
        {
          "id": "height_cm",
          "kind": "number",
          "title": "Height of the animal in CM",
          "hint": "Measure from the front knee to where the neck meets the body.",
          "required": false,
          "min": 10,
          "max": 200,
          "unit": "cm"
        },
        {
          "id": "rectal_temp_c",
          "kind": "number",
          "title": "Rectal temperature of the goat?",
          "required": true,
          "min": 30,
          "max": 45,
          "unit": "°C"
        },
        {
          "id": "temperature_media",
          "kind": "media",
          "title": "Rectal temperature media",
          "required": false,
          "slot": "temperature",
          "max_files": 1,
          "accepts": [
            "photo",
            "video"
          ]
        },
        {
          "id": "animal_media",
          "kind": "media",
          "title": "Goat photo and video with face, udder, body and activity",
          "hint": "Walk around the animal so the whole body is seen.",
          "required": true,
          "slot": "animal",
          "max_files": 5,
          "accepts": [
            "photo",
            "video"
          ]
        }
      ]
    },
    {
      "key": "face",
      "title": "Face visual productivity check",
      "questions": [
        {
          "id": "anaemic",
          "kind": "choice",
          "title": "Is the animal anaemic?",
          "required": true,
          "options": [
            {
              "value": "yes",
              "label": "Yes"
            },
            {
              "value": "no",
              "label": "No"
            }
          ]
        },
        {
          "id": "mouth_breathing",
          "kind": "choice",
          "title": "Is it breathing through its mouth?",
          "hint": "Yes for mouth, No for nose.",
          "required": true,
          "options": [
            {
              "value": "yes",
              "label": "Yes"
            },
            {
              "value": "no",
              "label": "No"
            }
          ]
        },
        {
          "id": "watery_eyes",
          "kind": "choice",
          "title": "Does it have watery eyes?",
          "hint": "If so, is it clear and watery or yellow and mucus like?",
          "required": true,
          "options": [
            {
              "value": "clear",
              "label": "Yes - clear and watery"
            },
            {
              "value": "yellow",
              "label": "Yes - yellow and mucus like"
            },
            {
              "value": "no",
              "label": "No"
            }
          ]
        },
        {
          "id": "eye_colour",
          "kind": "choice",
          "title": "Does it have red or cloudy eyes?",
          "required": true,
          "options": [
            {
              "value": "red",
              "label": "Yes - red eyes"
            },
            {
              "value": "cloudy",
              "label": "Yes - cloudy eyes"
            },
            {
              "value": "no",
              "label": "No"
            }
          ]
        },
        {
          "id": "nasal_discharge",
          "kind": "choice",
          "title": "Does it have a nasal discharge?",
          "hint": "If so, is it clear and runny or yellow and mucus like?",
          "required": true,
          "options": [
            {
              "value": "clear",
              "label": "Yes - clear and runny"
            },
            {
              "value": "yellow",
              "label": "Yes - yellow and mucus like"
            },
            {
              "value": "no",
              "label": "No"
            }
          ]
        },
        {
          "id": "face_scabs",
          "kind": "choice",
          "title": "Any signs of scabby skin or rashes on the face, even one small bump?",
          "hint": "Places: eyes, nose, outside mouth, ears, inside mouth around jaws and tongue.",
          "required": true,
          "options": [
            {
              "value": "no",
              "label": "No"
            },
            {
              "value": "other",
              "label": "Yes, please mention the area"
            }
          ],
          "allow_other": true
        }
      ]
    },
    {
      "key": "body",
      "title": "Body visual productivity check",
      "hint": "Checks for what is visible on the body, marked and rejected as per the SOP.",
      "questions": [
        {
          "id": "acidosis",
          "kind": "choice",
          "title": "Does the animal have acidosis?",
          "hint": "Is there a fluid sensation on pressing its stomach? It should feel doughy, like kneaded atta.",
          "required": true,
          "options": [
            {
              "value": "yes",
              "label": "Yes"
            },
            {
              "value": "no",
              "label": "No"
            }
          ]
        },
        {
          "id": "diarrhea",
          "kind": "choice",
          "title": "Any signs of diarrhea, present or past?",
          "hint": "Check below its tail and look for traces around both back legs.",
          "required": true,
          "options": [
            {
              "value": "yes",
              "label": "Yes"
            },
            {
              "value": "no",
              "label": "No"
            }
          ]
        },
        {
          "id": "ticks_hair_loss",
          "kind": "choice",
          "title": "Any presence of ticks or patches of hair loss on the body?",
          "hint": "Face | Neck | Visible trunk | Bottom trunk | Front legs (left and right + hoofs) | Rear legs (left and right + hoofs)",
          "required": true,
          "options": [
            {
              "value": "no",
              "label": "No"
            },
            {
              "value": "other",
              "label": "Yes, please mention where"
            }
          ],
          "allow_other": true
        },
        {
          "id": "wounds",
          "kind": "choice",
          "title": "Any wounds or physical injuries on the body?",
          "hint": "Face | Neck | Visible trunk | Bottom trunk | Front legs (left and right + hoofs) | Rear legs (left and right + hoofs)",
          "required": true,
          "options": [
            {
              "value": "no",
              "label": "No"
            },
            {
              "value": "other",
              "label": "Yes, please mention where"
            }
          ],
          "allow_other": true
        },
        {
          "id": "body_scabs",
          "kind": "choice",
          "title": "Any signs of scabby skin or rashes on the body, even one small bump?",
          "hint": "Places: neck and visible trunk, bottom of trunk, front legs and below hoof (both), rear legs and below hoof (both).",
          "required": true,
          "options": [
            {
              "value": "no",
              "label": "No"
            },
            {
              "value": "other",
              "label": "Yes, please mention where"
            }
          ],
          "allow_other": true
        },
        {
          "id": "lumps",
          "kind": "choice",
          "title": "Any signs of lumps on the face and body?",
          "hint": "Tick each of the points 1-14 and mention the numbers where a lump is present.",
          "required": true,
          "options": [
            {
              "value": "no",
              "label": "No"
            },
            {
              "value": "other",
              "label": "Yes, please mention where"
            }
          ],
          "allow_other": true
        },
        {
          "id": "arthritis",
          "kind": "choice",
          "title": "Any signs of arthritis in the knees?",
          "hint": "Look for swelling in the knees.",
          "required": true,
          "options": [
            {
              "value": "no",
              "label": "No"
            },
            {
              "value": "other",
              "label": "Yes, please mention where"
            }
          ],
          "allow_other": true
        },
        {
          "id": "suspicious_media",
          "kind": "media",
          "title": "If anything is suspicious, add a picture of it",
          "required": false,
          "slot": "suspicious",
          "max_files": 5,
          "accepts": [
            "photo",
            "video"
          ]
        }
      ]
    },
    {
      "key": "udder",
      "title": "Udder or testicles",
      "questions": [
        {
          "id": "udder_media",
          "kind": "media",
          "title": "Udder or testicles media",
          "required": true,
          "slot": "udder",
          "max_files": 1,
          "accepts": [
            "photo",
            "video"
          ]
        },
        {
          "id": "milk_yield",
          "kind": "text",
          "title": "Milk yield",
          "required": false,
          "only_if": {
            "question_id": "sex",
            "value": "female"
          }
        },
        {
          "id": "udder_state",
          "kind": "multi",
          "title": "Is the udder or testicle normal or has issues?",
          "required": true,
          "options": [
            {
              "value": "normal",
              "label": "Normal"
            },
            {
              "value": "swollen",
              "label": "Swollen"
            },
            {
              "value": "rashes",
              "label": "Rashes"
            },
            {
              "value": "lumps",
              "label": "Lumps"
            },
            {
              "value": "wounds",
              "label": "Wounds"
            },
            {
              "value": "teats_not_down",
              "label": "Teats not pointing down"
            },
            {
              "value": "other",
              "label": "Other"
            }
          ],
          "allow_other": true
        },
        {
          "id": "lactating",
          "kind": "choice",
          "title": "Is it lactating?",
          "required": true,
          "options": [
            {
              "value": "yes",
              "label": "Yes"
            },
            {
              "value": "no",
              "label": "No"
            }
          ],
          "only_if": {
            "question_id": "sex",
            "value": "female"
          }
        },
        {
          "id": "mastitis",
          "kind": "choice",
          "title": "If lactating, results of mastitis test are?",
          "required": false,
          "options": [
            {
              "value": "positive",
              "label": "Positive"
            },
            {
              "value": "negative",
              "label": "Negative"
            }
          ],
          "only_if": {
            "question_id": "lactating",
            "value": "yes"
          }
        },
        {
          "id": "teats",
          "kind": "choice",
          "title": "How many teats are present?",
          "required": true,
          "options": [
            {
              "value": "1",
              "label": "1"
            },
            {
              "value": "2",
              "label": "2"
            },
            {
              "value": "3",
              "label": "3"
            },
            {
              "value": "4",
              "label": "4"
            }
          ],
          "only_if": {
            "question_id": "sex",
            "value": "female"
          }
        },
        {
          "id": "teat_discharge",
          "kind": "choice",
          "title": "Any pus like discharge from the teats that does not look like milk or colostrum?",
          "required": true,
          "options": [
            {
              "value": "yes",
              "label": "Yes"
            },
            {
              "value": "no",
              "label": "No"
            }
          ],
          "only_if": {
            "question_id": "sex",
            "value": "female"
          }
        },
        {
          "id": "scrotum_cm",
          "kind": "number",
          "title": "Male scrotum circumference in CM",
          "required": false,
          "min": 5,
          "max": 60,
          "unit": "cm",
          "only_if": {
            "question_id": "sex",
            "value": "male"
          }
        }
      ]
    },
    {
      "key": "decision",
      "title": "Your verdict",
      "hint": "Your recommendation on the farm. The CEO decides on the web.",
      "questions": [
        {
          "id": "field_verdict",
          "kind": "choice",
          "title": "Decision",
          "required": true,
          "options": [
            {
              "value": "selected",
              "label": "Selected"
            },
            {
              "value": "on_hold",
              "label": "On Hold"
            }
          ]
        },
        {
          "id": "breed",
          "kind": "text",
          "title": "Breed",
          "required": false
        },
        {
          "id": "notes",
          "kind": "text",
          "title": "Note",
          "required": false
        }
      ]
    }
  ],
  "schema_version": "goatos.sop-inspection.v1"
}$seed$::jsonb
),
'{"subject_scope": "task", "types": ["video", "photo"], "required": true, "minimum_count": 3, "maximum_count": 20, "verify_before_apply": false}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["video.capture", "photo.capture"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl.inspection", "message": "Seeded from the code catalog this version replaces (migration 000304)."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'procurement.animal_purchase'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
-- Forward-only: the document is the day-one inspection; retiring it is a maintainer decision.
