# Goat Accelerometer Behaviour Paper Notes

Source paper: Sarah Mauny, Joon Kwon, Nicolas C. Friggens, Christine Duvaux-Ponter, and Masoomeh Taghipoor. "A pipeline with pre-processing options to detect behaviour from accelerometer data using Machine Learning tested on dairy goats." Peer Community Journal, 5:e41, 2025.

- Article: https://peercommunityjournal.org/articles/10.24072/pcjournal.545/
- DOI: https://doi.org/10.24072/pcjournal.545
- Dataset: https://doi.org/10.57745/LGZBM1
- Code / ACT4Behav pipeline: https://doi.org/10.5281/zenodo.12624796
- Supplementary information: https://doi.org/10.5281/zenodo.15148455
- Related data paper: https://doi.org/10.1016/j.anopes.2025.100095

## Why This Is Relevant To Mesha

This is one of the most directly useful papers for Mesha's indoor goat/sheep sensing idea because it tested:

- indoor-housed goats;
- ear-mounted accelerometers;
- overhead shed video;
- human-labelled behaviour;
- machine learning detection of feeding and welfare-relevant behaviours.

This is close to the desired Mesha setup:

```text
RFID identity + ear/neck IMU sensor + shed camera + Goat OS labels
= stall-fed small-ruminant behaviour intelligence
```

The paper supports a pilot focused on detecting:

- head in feeder;
- rumination;
- standing;
- lying;
- changes in feeding/activity that may indicate sickness, lameness, stress, recovery, or poor welfare.

It does not prove that an Alibaba sensor vendor's built-in AI will work. It proves that raw accelerometer data plus video labels can be turned into useful goat behaviour models.

## Study Setup

| Field | Detail |
|---|---|
| Species | Dairy goats |
| Breed/type | Alpine dairy goats |
| Number of goats | 8 |
| Age/status | 14-month-old lactating primiparous goats |
| Housing | Indoor, group-housed in one slatted-floor pen during data collection |
| Duration | 24 hours of accelerometer data |
| Video labelling window | About 11 daylight hours per goat |
| Feed access | Each goat had its own feed trough, released by electronic ear tag near an antenna |
| Sensor | MSR145 3D accelerometer |
| Sensor placement | Attached to the goat's RFID ear tag |
| Sensor size | 27 x 16 x 53 mm |
| Sensor weight | About 20 g |
| Sensor battery | 230 mAh lithium-polymer battery |
| Sampling frequency | 5 Hz |
| Video setup | Cameras above pens |
| Video labels | Single trained observer using The Observer XT |

## Behaviour Labels

The ethogram grouped behaviours into:

- feeding behaviour;
- social behaviour, such as grooming or fighting;
- position and movement behaviour, such as standing, walking, lying down;
- other behaviour;
- disturbance events.

The paper selected four behaviours for ML classification because they were common enough in the data and relevant to health/welfare:

| Behaviour | Mesha relevance |
|---|---|
| Rumination | Digestion, feed response, sickness/stress warning |
| Head in the feeder | Direct stall-feeding engagement signal |
| Standing | Activity/posture baseline |
| Lying | Rest, welfare, sickness/lameness/recovery pattern |

Definitions for Mesha use:

- Eating/head in feeder = animal is actively at the feed point.
- Rumination = animal re-chews cud after swallowing; a strong digestion/health signal.
- Standing/lying = posture budget; changes can indicate welfare or health issues.

## Video Identification Method

The researchers painted a number on each goat's back with yellow animal spray paint so the observer could distinguish animals in overhead video.

Mesha implication:

- Spray paint is useful only for short pilot video labelling.
- It should not be the permanent identity system.
- Use RFID/visual ear tag as the true identity and spray paint only as a temporary camera-visible backup.

Likely farm reality:

- livestock spray may last days to a few weeks depending product;
- dirt, rubbing, washing, and hair shedding will reduce life;
- in Mesha sheds, assume repainting may be needed frequently during a video-labelling pilot.

## Modelling Approach

The authors developed and tested ACT4Behav: Accelerometer-based Classification Tool for identifying Behaviours.

## Algorithm / Data Pipeline Documented In The Paper

The paper does document the algorithmic direction. It is not firmware, and it is not a ready mobile/production classifier, but it gives a reproducible ML pipeline and links the code.

High-level flow:

```text
raw accelerometer x/y/z at 5 Hz
  -> optional derived signals
  -> split into fixed time windows
  -> assign behaviour label per window from video
  -> calculate many time-series features
  -> select most important features
  -> train one binary classifier per behaviour
  -> evaluate on held-out windows and held-out goats
```

Detailed flow:

| Step | What they did |
|---|---|
| 1. Collect raw acceleration | Ear-mounted MSR145 records x/y/z acceleration at 5 Hz |
| 2. Synchronise with video | Shake accelerometer in front of camera to align sensor time with video time |
| 3. Label video | Human observer labels goat behaviours in The Observer XT |
| 4. Convert to binary tasks | One model per behaviour: rumination yes/no, head-in-feeder yes/no, lying yes/no, standing yes/no |
| 5. Window data | Split sensor stream into fixed windows: 10, 20, 30, 40, 50, 60, 80, 120 seconds tested |
| 6. Assign window label | Behaviour label is 1 if that behaviour occurs for more than 50% of the window |
| 7. Add derived time series | Calculate norm, pitch, roll, and rotated acceleration variants |
| 8. Extract features | Use Python `tsfresh` to calculate 777 time-series features per signal/window |
| 9. Feature selection | Rank features by importance and test top 2000, 1000, 500, 100, 80, 50, 20, 10 |
| 10. Train classifier | Use CatBoost / gradient boosting |
| 11. Tune model | Use validation data and AUC to choose preprocessing settings |
| 12. Test model | Test on unseen time windows and separately on unseen goats |

Important implementation choices:

- They did **not** classify directly from raw x/y/z values.
- They converted raw signals into time-window features.
- They trained separate binary models per behaviour instead of one big behaviour classifier.
- They tested generalisation to new goats, which is the right test for real deployment.
- The linked ACT4Behav code is the starting point if Mesha wants to reproduce the pipeline.

Mesha engineering implication:

```text
If a BLE ear tag only gives "activity score", we cannot reproduce this pipeline.
If it gives raw x/y/z acceleration at usable frequency, we can build Mesha's own classifier.
```

Model framing:

- one binary classifier per behaviour;
- label 1 if the behaviour occupied more than 50% of the time window;
- CatBoost / gradient boosting used for classification;
- performance mainly evaluated with AUC because behaviours are imbalanced and AUC is less dependent on a single threshold.

Data split strategies:

1. Time-window split:
   - data from all goats appears in training/test but different time windows are held out;
   - answers: can the model detect behaviour on the same animals in the same context?

2. Goat-based split:
   - train on six goats and test on two goats;
   - answers: can the model generalise to goats it has not seen?

The goat-based split is more important for Mesha because a real system cannot require manual labelling for every single animal forever.

## Pre-Processing Tested

The paper compared several pre-processing choices:

| Step | Options tested |
|---|---|
| Time-window size | 10, 20, 30, 40, 50, 60, 80, 120 seconds |
| Raw acceleration filtering | no filtering; high-pass filters at 0.01, 0.05, 0.1, 0.2, 0.3, 0.4 Hz |
| Additional time series | norm, pitch, roll, rotated acceleration using mean or median orientation |
| Feature extraction | 777 time-series features using tsfresh |
| Feature selection | no selection, or top 2000, 1000, 500, 100, 80, 50, 20, 10 features |

Important methods:

- Norm of acceleration was used to reduce dependence on sensor orientation.
- Pitch and roll helped because head/body tilt differs between feeding, standing, lying, and rumination.
- Rotated acceleration data was used to standardise sensor orientation.
- The ear is mobile, so orientation correction matters.

Mesha hardware implication:

- Ask sensor vendors for raw x/y/z accelerometer data if possible.
- If buying BLE tags, confirm whether they expose raw acceleration or only a processed "activity score".
- Processed activity score alone is much weaker for Mesha IP.

## Best Results In Same-Goat Time-Window Split

| Behaviour | Best time window | Filtering | Extra time series | Feature selection | AUC | Accuracy | Sensitivity | Specificity |
|---|---:|---|---|---|---:|---:|---:|---:|
| Rumination | 20 s | none | median rotated acceleration + Euler angles | top 100 | 0.800 | 78.7% | 54.2% | 58.6% |
| Head in feeder | 60 s | none | mean rotated acceleration + Euler angles | top 80 | 0.819 | 73.7% | 74.3% | 62.2% |
| Lying | 50 s | none | Euler angles | top 80 | 0.829 | 78.1% | 69.8% | 71.3% |
| Standing | 60 s | none | no extra time series listed in table | top 80 | 0.823 | 74.6% | 95.4% | 71.2% |

Key findings:

- AUC around 0.80 is useful but not perfect.
- No high-pass filtering worked best for all four behaviours.
- Pitch/roll or orientation-related features helped.
- Head-in-feeder and lying were slightly stronger than rumination.
- Rumination sensitivity was modest, so rumination is harder than simply detecting feeder/head posture.

## Generalisation To New Goats

When the model was trained on six goats and tested on two different goats, performance dropped:

| Behaviour | Time-window split AUC | Goat-split AUC | Drop |
|---|---:|---:|---:|
| Rumination | 0.800 | 0.644 | -19.5% |
| Head in feeder | 0.819 | 0.733 | -10.5% |
| Lying | 0.829 | 0.741 | -10.6% |
| Standing | 0.823 | 0.749 | -9.0% |

This is the most important caution in the paper.

Mesha implication:

- A small pilot can prove feasibility but will not produce a robust product model.
- Behaviour differs animal-to-animal.
- Need data across many goats/sheep, sheds, ages, breeds, seasons, feed routines, sick/healthy states, pregnancy/lactation states, and sensor placements.
- Vendor claims trained on cattle or generic livestock should not be trusted for Mesha goats/sheep.

## Feature Importance

The paper found different important features per behaviour:

| Behaviour | Important feature type |
|---|---|
| Rumination | repeated-value patterns on rotated x-axis acceleration |
| Head in feeder | permutation entropy on rotated z-axis acceleration |
| Lying | permutation entropy on acceleration norm |
| Standing | Lempel-Ziv complexity estimate on z-axis acceleration |

Plain-English interpretation:

- Static posture and repeated movement patterns matter.
- Complexity/unpredictability of movement helps separate behaviours.
- Feeding, rumination, standing, and lying are not detected by one simple "activity" value.
- Raw time-series features are valuable.

## Limitations

| Limitation | Why it matters |
|---|---|
| Only 8 goats | Too small for production-grade generalisation |
| One farm/context | Slatted floor, specific feed setup, specific breed/status |
| Only daylight video labels | Does not prove 24-hour camera-labelled behaviour |
| Ear-mounted sensor only | Results may differ for collar, leg, or other placements |
| Behaviour set limited | Rare/transitional behaviours could not be modelled |
| Same-animal scores better than new-animal scores | Product must train/test across many unseen animals |
| Rumination sensitivity modest | Health inference should use multiple signals, not rumination alone |

## Practical Mesha Pilot Design From This Paper

Recommended pilot:

```text
50-100 animals
goats + sheep if possible
ear accelerometer/BLE tag samples
RFID identity mapping
fixed camera over feed line / shed
manual video labels for selected windows
Goat OS events: health, feeding, weighing, treatment, pregnancy/lactation, ICU/quarantine
```

Minimum labels to collect:

- head in feeder;
- eating/feeding attempt;
- rumination;
- lying;
- standing;
- walking/restless;
- isolation/low activity;
- operator-observed not-eating;
- confirmed sickness/treatment;
- recovery after treatment.

Do not start with GPS/solar/virtual fencing for Mesha's stall-fed animals.

## Hardware Requirements Implied By This Paper

For ear tags or collars:

- 3-axis accelerometer is essential.
- Gyroscope is useful but this paper used accelerometer only.
- Raw x/y/z export is strongly preferred.
- Sampling around 5 Hz is enough for this paper's setup.
- Sensor weight around 20 g was used on goats in the experiment.
- Camera labels are needed to build Mesha's own models.
- Battery life must be tested under actual sampling/upload settings.

Ask vendors:

```text
Can the tag export raw x/y/z accelerometer data?
What is the sampling rate?
Can it sample at 5 Hz or higher?
Can it batch/store data and upload later?
Can it map records to RFID animal ID?
What is tag weight in grams?
What is battery life at the sampling/upload mode we need?
Is temperature raw data available or only alerts?
```

## What This Paper Does Not Prove

It does not prove:

- breeding/heat detection in goats/sheep;
- sickness diagnosis;
- pregnancy detection;
- continuous production deployment;
- sheep performance;
- vendor algorithm accuracy;
- that BLE ear tags on Alibaba will expose usable raw data;
- that one model works for every farm.

It supports the first step:

```text
ear accelerometer + video labels can detect useful indoor goat behaviours
```

## Mesha Interpretation

This paper should be treated as evidence for building Mesha's own stall-fed small-ruminant behaviour dataset.

Best product direction:

```text
Goat OS sensor layer
  RFID identity
  BLE/IMU ear or neck sensor
  shed/feed-line camera
  weighing records
  feed-direction records
  health/treatment events
  human/video verification labels

Mesha IP
  feeding engagement model
  rumination/rest/posture model
  abnormal baseline drift model
  sick/not-eating early warning
  recovery monitoring
  cohort/shed risk dashboard
```

The paper is useful and should remain in the research pack.
