package app

import "github.com/vgoats/goatos/backend/internal/workboard/domain"

// EVERY MODULE IS ON THE WORK BOARD OR SAYS WHY NOT (maintainer instruction 2026-09-25:
// "procurement and toxin testing are not linked to the work board ... in future also, if I have
// any task, any new module, it should automatically link to the work board; it should not be one
// more task").
//
// Procurement and Toxin had a lane on the board from day one and NO source feeding it, so the
// board silently hid them for two weeks: nothing failed, nothing said so. This table closes that
// door for every module the farm can be given access to (permissions.ModuleCapabilities):
//
//   - a module that owns farm WORK names the board lane(s) its work rows under, and
//     TestEveryWorkBoardLaneHasASource (bootstrap) fails the build if a named lane has no source;
//   - a module that owns NO work -- a register, a lens, a settings screen -- says why, in words.
//
// A module added to the capability catalog without an entry here fails
// TestEveryModuleDeclaresItsWorkBoardLane. A module that runs on the shared tasks engine (the
// rule for every new operational feature) needs NO source of its own: the engine's source rows
// every workflow, and names its lane in tasks/adapters/boardsource.engineModuleLanes.
var moduleLanes = map[string][]domain.Module{
	"vaccination":      {domain.ModuleVaccination},
	"weighing":         {domain.ModuleWeighing},
	"counts":           {domain.ModuleCounts},
	"approvals":        {domain.ModuleCounts},
	"feed_direction":   {domain.ModuleFeed},
	"milk":             {domain.ModuleMilk},
	"aas_health":       {domain.ModuleHealth},
	"pc_care":          {domain.ModulePCCare},
	"pc_trimming":      {domain.ModulePCCare},
	"procurement":      {domain.ModuleProcurement},
	"animal_purchases": {domain.ModuleProcurement},
	"feed_purchases":   {domain.ModuleProcurement},
	"toxin":            {domain.ModuleToxin},
	"sales":            {domain.ModuleSales},
	"sale_allocation":  {domain.ModuleSales},
	"verification":     {domain.ModuleVerification},
	"pen_routines":     {domain.ModuleTasks},
}

// notBoardWork is every module that owns no work a person owes on a park-day, with the reason.
// Adding a module here is a statement that it has no work; if it later grows some, it moves to
// moduleLanes in the same change.
var notBoardWork = map[string]string{
	"breeding":                 "Not built yet: it opens no work. When it does, it runs on the tasks engine and rows on the board through it.",
	"leave_approvals":          "Leave requests are a person's time off, decided on their own queue; they are not farm work on a park-day.",
	"vendors":                  "A register of suppliers and buyers; recording one owes no work.",
	"market_survey":            "A price record typed when the morning calls are made; nothing is owed per park-day.",
	"people":                   "The staff directory and access settings.",
	"config":                   "The standing rules and written procedures work follows, not work itself.",
	"herd_register":            "The animal register; its changes come through Herd operations, which rows under Counts.",
	"locations":                "The park and pen directory.",
	"calendar":                 "A lens over planned work that the board already carries module by module.",
	"work_board":               "The board itself.",
	"alerts":                   "A lens over what is off today; the work behind each alert is on the board under its own module.",
	"leadership_tasks":         "Person-to-person tasks with their own list; the board's flag raises them rather than showing them.",
	"leadership_tasks_monitor": "Oversight of the person-to-person task list.",
	"configuration":            "Farm places, animal types and catalogues: the lists every screen picks from.",
	"clock":                    "Clock-ins and hours, not work on a pen.",
	"load_costs":               "Money figures on a purchased load; the load's work rows under Procurement.",
	"verification_policy":      "How much proof gets watched; the proofs themselves row under Verification.",
	"herd_signals":             "Sensor readings from the herd.",
	"operations":               "Replaying failed background work. Engineering use.",
}

// ModuleLanes is the declaration read by the build checks.
func ModuleLanes() (lanes map[string][]domain.Module, excluded map[string]string) {
	lanes = make(map[string][]domain.Module, len(moduleLanes))
	for k, v := range moduleLanes {
		lanes[k] = append([]domain.Module(nil), v...)
	}
	excluded = make(map[string]string, len(notBoardWork))
	for k, v := range notBoardWork {
		excluded[k] = v
	}
	return lanes, excluded
}
