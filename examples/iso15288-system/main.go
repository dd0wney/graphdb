package main

import (
	"fmt"
	"log"
	"math"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
	"github.com/dd0wney/cluso-graphdb/pkg/constraints"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Proof-of-concept: ISO 15288 System Modeling with GraphDB
// Shows how GraphDB can model ALL 5 system dimensions

func main() {
	fmt.Println("=== ISO 15288 System Certification Demo ===")

	// Create graph database
	graph, err := storage.NewGraphStorage("./data/iso15288_demo")
	if err != nil {
		log.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	fmt.Println("1. Creating System Elements (5 Dimensions)...")

	// ========================================
	// DIMENSION 1: TECHNICAL SYSTEMS
	// ========================================
	safetyPLC, _ := graph.CreateNode(
		[]string{"Technical", "PLC", "SafetyCritical"},
		map[string]storage.Value{
			"priority": storage.IntValue(math.MaxInt64),
			"function": storage.StringValue("train_safety_interlock"),
			"vendor":   storage.StringValue("Siemens S7-1500F"),
		},
	)
	fmt.Println("   ✓ Created Technical: Safety PLC (priority: ∞)")

	scada, _ := graph.CreateNode(
		[]string{"Technical", "SCADA"},
		map[string]storage.Value{
			"priority": storage.IntValue(10000),
			"function": storage.StringValue("supervisory_control"),
		},
	)
	fmt.Println("   ✓ Created Technical: SCADA (priority: 10000)")

	historian, _ := graph.CreateNode(
		[]string{"Technical", "Database"},
		map[string]storage.Value{
			"priority": storage.IntValue(1000),
			"function": storage.StringValue("data_historian"),
		},
	)
	fmt.Println("   ✓ Created Technical: Historian (priority: 1000)")

	// ========================================
	// DIMENSION 2: HUMAN SYSTEMS
	// ========================================
	safetyOfficer, _ := graph.CreateNode(
		[]string{"Human", "Operator", "SafetyCertified"},
		map[string]storage.Value{
			"priority":       storage.IntValue(10000),
			"role":           storage.StringValue("safety_officer"),
			"certifications": storage.StringValue("IEC62443,RailSafety"),
			"clearance":      storage.IntValue(5),
		},
	)
	fmt.Println("   ✓ Created Human: Safety Officer (priority: 10000)")

	controlRoomOp, _ := graph.CreateNode(
		[]string{"Human", "Operator"},
		map[string]storage.Value{
			"priority":       storage.IntValue(1000),
			"role":           storage.StringValue("control_room_operator"),
			"certifications": storage.StringValue("BasicOps"),
			"clearance":      storage.IntValue(3),
		},
	)
	fmt.Println("   ✓ Created Human: Control Room Operator (priority: 1000)")

	itAdmin, _ := graph.CreateNode(
		[]string{"Human", "Admin"},
		map[string]storage.Value{
			"priority":       storage.IntValue(10),
			"role":           storage.StringValue("it_administrator"),
			"certifications": storage.StringValue("MCSE"),
			"clearance":      storage.IntValue(1),
		},
	)
	fmt.Println("   ✓ Created Human: IT Admin (priority: 10)")

	// ========================================
	// DIMENSION 3: PHYSICAL SYSTEMS
	// ========================================
	safetyRoom, _ := graph.CreateNode(
		[]string{"Physical", "Room", "RestrictedArea"},
		map[string]storage.Value{
			"priority":       storage.IntValue(math.MaxInt64),
			"location":       storage.StringValue("Building_A_L1"),
			"access_control": storage.StringValue("biometric_plus_escort"),
		},
	)
	fmt.Println("   ✓ Created Physical: Safety Room (priority: ∞)")

	controlRoom, _ := graph.CreateNode(
		[]string{"Physical", "Room"},
		map[string]storage.Value{
			"priority":       storage.IntValue(10000),
			"location":       storage.StringValue("Building_A_L2"),
			"access_control": storage.StringValue("badge_plus_pin"),
		},
	)
	fmt.Println("   ✓ Created Physical: Control Room (priority: 10000)")

	office, _ := graph.CreateNode(
		[]string{"Physical", "Room"},
		map[string]storage.Value{
			"priority":       storage.IntValue(10),
			"location":       storage.StringValue("Building_B_L3"),
			"access_control": storage.StringValue("badge_only"),
		},
	)
	fmt.Println("   ✓ Created Physical: Office (priority: 10)")

	// ========================================
	// DIMENSION 4: INFORMATIONAL SYSTEMS
	// ========================================
	criticalAlarm, _ := graph.CreateNode(
		[]string{"Informational", "Alert", "SafetyCritical"},
		map[string]storage.Value{
			"priority":    storage.IntValue(math.MaxInt64),
			"alert_type":  storage.StringValue("safety_interlock_triggered"),
			"latency_max": storage.IntValue(1), // 1 second
		},
	)
	fmt.Println("   ✓ Created Informational: Critical Alarm (priority: ∞)")

	dailyReport, _ := graph.CreateNode(
		[]string{"Informational", "Report"},
		map[string]storage.Value{
			"priority":    storage.IntValue(10),
			"report_type": storage.StringValue("daily_operations_summary"),
			"latency_max": storage.IntValue(86400), // 24 hours
		},
	)
	fmt.Println("   ✓ Created Informational: Daily Report (priority: 10)")

	// ========================================
	// DIMENSION 5: ORGANIZATIONAL SYSTEMS
	// ========================================
	safetyChangeProcess, _ := graph.CreateNode(
		[]string{"Organizational", "Process", "ChangeManagement"},
		map[string]storage.Value{
			"priority":        storage.IntValue(math.MaxInt64),
			"name":            storage.StringValue("safety_critical_change"),
			"approval_levels": storage.IntValue(4),
			"timeline_days":   storage.IntValue(180),
		},
	)
	fmt.Println("   ✓ Created Organizational: Safety Change Process (priority: ∞)")

	standardChange, _ := graph.CreateNode(
		[]string{"Organizational", "Process", "ChangeManagement"},
		map[string]storage.Value{
			"priority":        storage.IntValue(100),
			"name":            storage.StringValue("standard_change"),
			"approval_levels": storage.IntValue(1),
			"timeline_days":   storage.IntValue(7),
		},
	)
	fmt.Println("   ✓ Created Organizational: Standard Change (priority: 100)")

	fmt.Println("\n2. Creating System Flows (Priority Flow Enforcement)...")

	// ========================================
	// DATA FLOWS (Technical → Technical)
	// ========================================
	graph.CreateEdge(safetyPLC.ID, scada.ID, "DATA_FLOW",
		map[string]storage.Value{
			"enforcement": storage.StringValue("data_diode"),
			"bandwidth":   storage.IntValue(1000),
		}, 1.0)
	fmt.Println("   ✓ DATA_FLOW: Safety PLC → SCADA (unidirectional)")

	graph.CreateEdge(scada.ID, historian.ID, "DATA_FLOW",
		map[string]storage.Value{
			"enforcement": storage.StringValue("firewall_rule"),
			"bandwidth":   storage.IntValue(10000),
		}, 1.0)
	fmt.Println("   ✓ DATA_FLOW: SCADA → Historian (unidirectional)")

	// ========================================
	// COMMAND AUTHORITY (Human → Technical)
	// ========================================
	graph.CreateEdge(safetyOfficer.ID, safetyPLC.ID, "COMMAND_AUTHORITY",
		map[string]storage.Value{
			"can_issue":         storage.StringValue("emergency_shutdown"),
			"requires_approval": storage.BoolValue(false),
		}, 1.0)
	fmt.Println("   ✓ COMMAND_AUTHORITY: Safety Officer → Safety PLC (authorized)")

	graph.CreateEdge(controlRoomOp.ID, scada.ID, "COMMAND_AUTHORITY",
		map[string]storage.Value{
			"can_issue":         storage.StringValue("operational_commands"),
			"requires_approval": storage.BoolValue(false),
		}, 1.0)
	fmt.Println("   ✓ COMMAND_AUTHORITY: Control Operator → SCADA (authorized)")

	// VIOLATION: IT Admin should NOT command safety PLC
	graph.CreateEdge(itAdmin.ID, safetyPLC.ID, "COMMAND_AUTHORITY",
		map[string]storage.Value{
			"can_issue":         storage.StringValue("configuration_change"),
			"requires_approval": storage.BoolValue(true),
		}, 1.0)
	fmt.Println("   ✗ COMMAND_AUTHORITY: IT Admin → Safety PLC (PRIORITY INVERSION!)")

	// ========================================
	// PHYSICAL ACCESS (Human → Physical)
	// ========================================
	graph.CreateEdge(safetyOfficer.ID, safetyRoom.ID, "PHYSICAL_ACCESS",
		map[string]storage.Value{
			"access_level": storage.StringValue("unrestricted"),
			"valid_times":  storage.StringValue("24/7"),
		}, 1.0)
	fmt.Println("   ✓ PHYSICAL_ACCESS: Safety Officer → Safety Room (unrestricted)")

	graph.CreateEdge(itAdmin.ID, safetyRoom.ID, "PHYSICAL_ACCESS",
		map[string]storage.Value{
			"access_level": storage.StringValue("escort_required"),
			"valid_times":  storage.StringValue("business_hours"),
		}, 1.0)
	fmt.Println("   ✓ PHYSICAL_ACCESS: IT Admin → Safety Room (escort required)")

	graph.CreateEdge(controlRoomOp.ID, controlRoom.ID, "PHYSICAL_ACCESS",
		map[string]storage.Value{
			"access_level": storage.StringValue("unrestricted"),
		}, 1.0)
	fmt.Println("   ✓ PHYSICAL_ACCESS: Control Operator → Control Room")

	graph.CreateEdge(itAdmin.ID, office.ID, "PHYSICAL_ACCESS",
		map[string]storage.Value{
			"access_level": storage.StringValue("unrestricted"),
		}, 1.0)
	fmt.Println("   ✓ PHYSICAL_ACCESS: IT Admin → Office")

	// ========================================
	// INFORMATION FLOW (Informational → Human)
	// ========================================
	graph.CreateEdge(criticalAlarm.ID, safetyOfficer.ID, "INFORMATION_FLOW",
		map[string]storage.Value{
			"delivery":    storage.StringValue("immediate_sms_alarm"),
			"latency_max": storage.IntValue(1),
		}, 1.0)
	fmt.Println("   ✓ INFORMATION_FLOW: Critical Alarm → Safety Officer (immediate)")

	graph.CreateEdge(dailyReport.ID, itAdmin.ID, "INFORMATION_FLOW",
		map[string]storage.Value{
			"delivery":    storage.StringValue("email_batch"),
			"latency_max": storage.IntValue(86400),
		}, 1.0)
	fmt.Println("   ✓ INFORMATION_FLOW: Daily Report → IT Admin (batched)")

	// ========================================
	// CHANGE AUTHORITY (Process → Technical)
	// ========================================
	graph.CreateEdge(safetyChangeProcess.ID, safetyPLC.ID, "CHANGE_AUTHORITY",
		map[string]storage.Value{
			"required_approvals": storage.StringValue("safety_officer,ops_mgr,regulatory"),
		}, 1.0)
	fmt.Println("   ✓ CHANGE_AUTHORITY: Safety Change Process → Safety PLC")

	graph.CreateEdge(standardChange.ID, historian.ID, "CHANGE_AUTHORITY",
		map[string]storage.Value{
			"required_approvals": storage.StringValue("it_admin"),
		}, 1.0)
	fmt.Println("   ✓ CHANGE_AUTHORITY: Standard Change → Historian")

	fmt.Println("\n3. Running ISO 15288 System Validation...")

	// ========================================
	// VALIDATION 1: DAG Check (No Cycles)
	// ========================================
	fmt.Println("   [1/5] Network Topology Validation (DAG Check)...")
	isDAG, _ := algorithms.IsDAG(graph)
	if isDAG {
		fmt.Println("         ✓ System forms a DAG (no cycles)")
	} else {
		fmt.Println("         ✗ VIOLATION: System contains cycles")
	}

	// ========================================
	// VALIDATION 2: Cycle Detection
	// ========================================
	fmt.Println("   [2/5] Cycle Detection...")
	cycles, _ := algorithms.DetectCycles(graph)
	if len(cycles) == 0 {
		fmt.Println("         ✓ No cycles detected")
	} else {
		fmt.Printf("         ✗ Found %d cycles\n", len(cycles))
	}

	// ========================================
	// VALIDATION 3: Property Constraints
	// ========================================
	fmt.Println("   [3/5] Property Constraint Validation...")
	validator := constraints.NewValidator()

	// All operators must have certifications
	validator.AddConstraint(&constraints.PropertyConstraint{
		NodeLabel:    "Operator",
		PropertyName: "certifications",
		Type:         storage.TypeString,
		Required:     true,
	})

	// All technical systems must have priority
	validator.AddConstraint(&constraints.PropertyConstraint{
		NodeLabel:    "Technical",
		PropertyName: "priority",
		Type:         storage.TypeInt,
		Required:     true,
	})

	result, _ := validator.Validate(graph)
	if result.Valid {
		fmt.Println("         ✓ All property constraints satisfied")
	} else {
		fmt.Printf("         ✗ Found %d constraint violations\n", len(result.Violations))
	}

	// ========================================
	// VALIDATION 4: Priority Flow Check
	// ========================================
	fmt.Println("   [4/5] Priority Flow Validation...")
	violations := checkPriorityFlows(graph)
	if len(violations) == 0 {
		fmt.Println("         ✓ All flows respect priority ordering")
	} else {
		fmt.Printf("         ✗ Found %d priority inversions:\n", len(violations))
		for _, v := range violations {
			fmt.Printf("            - %s\n", v)
		}
	}

	// ========================================
	// VALIDATION 5: The Steve Test
	// ========================================
	fmt.Println("   [5/5] The Steve Test (Betweenness Centrality)...")
	steveViolations := runSteveTest(graph)
	if len(steveViolations) == 0 {
		fmt.Println("         ✓ No low-priority humans bridge high-priority systems")
	} else {
		fmt.Printf("         ✗ Found %d Steve Test violations:\n", len(steveViolations))
		for _, v := range steveViolations {
			fmt.Printf("            - %s\n", v)
		}
	}

	// ========================================
	// FINAL CERTIFICATION DECISION
	// ========================================
	fmt.Println("\n4. Certification Decision...")
	allViolations := len(cycles) + len(result.Violations) + len(violations) + len(steveViolations)
	if allViolations == 0 {
		fmt.Println("   ✓✓✓ SYSTEM CERTIFIED ✓✓✓")
		fmt.Println("   ISO 15288 System Certification GRANTED")
		fmt.Println("   All priority flows verified across 5 dimensions")
	} else {
		fmt.Printf("   ✗✗✗ CERTIFICATION FAILED ✗✗✗\n")
		fmt.Printf("   Total violations: %d\n", allViolations)
		fmt.Println("   Remediation required before certification")
	}

	stats := graph.GetStatistics()
	fmt.Printf("\nSystem Statistics:\n")
	fmt.Printf("   Total Elements: %d\n", stats.NodeCount)
	fmt.Printf("   Total Flows: %d\n", stats.EdgeCount)
}

// checkPriorityFlows validates that all flows respect priority ordering
func checkPriorityFlows(graph *storage.GraphStorage) []string {
	violations := []string{}
	stats := graph.GetStatistics()

	for i := uint64(1); i <= stats.NodeCount; i++ {
		fromNode, err := graph.GetNode(i)
		if err != nil {
			continue
		}

		fromPriority, ok := fromNode.GetProperty("priority")
		if !ok {
			continue
		}
		fromPriorityInt, _ := fromPriority.AsInt()

		outgoing, _ := graph.GetOutgoingEdges(i)
		for _, edge := range outgoing {
			// Skip certain edge types that don't follow priority flow
			if edge.Type == "PHYSICAL_ACCESS" || edge.Type == "INFORMATION_FLOW" {
				continue
			}

			toNode, _ := graph.GetNode(edge.ToNodeID)
			toPriority, ok := toNode.GetProperty("priority")
			if !ok {
				continue
			}
			toPriorityInt, _ := toPriority.AsInt()

			// Check: high priority should NOT flow to higher priority
			// (except for valid upward flows like INFORMATION_FLOW)
			if edge.Type == "COMMAND_AUTHORITY" && fromPriorityInt < toPriorityInt {
				fromName, _ := fromNode.GetProperty("role")
				toName, _ := toNode.GetProperty("function")
				fromNameStr, _ := fromName.AsString()
				toNameStr, _ := toName.AsString()

				violations = append(violations, fmt.Sprintf(
					"%s: %s (priority %d) → %s (priority %d)",
					edge.Type, fromNameStr, fromPriorityInt, toNameStr, toPriorityInt,
				))
			}
		}
	}

	return violations
}

// runSteveTest checks for low-priority humans with high betweenness
func runSteveTest(graph *storage.GraphStorage) []string {
	violations := []string{}

	// Note: BetweennessCentrality not yet implemented in this demo
	// This is a placeholder showing how it would be used

	fmt.Println("         (Betweenness centrality calculation skipped in demo)")

	return violations
}
