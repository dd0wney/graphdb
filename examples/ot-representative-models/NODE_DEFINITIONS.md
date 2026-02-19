# Node Definitions Reference for cluso-graphdb Implementation

## MODEL 1: Steve's Utility (33 nodes)

### Technical Nodes (22)

| Name | Labels | Level | Node Type |
|------|--------|-------|-----------|
| PLC_Turbine1 | Technical, PLC | L0_Process | technical |
| PLC_Turbine2 | Technical, PLC | L0_Process | technical |
| PLC_Substation | Technical, PLC | L0_Process | technical |
| RTU_Remote1 | Technical, RTU | L0_Process | technical |
| RTU_Remote2 | Technical, RTU | L0_Process | technical |
| HMI_Control1 | Technical, HMI | L1_Control | technical |
| HMI_Control2 | Technical, HMI | L1_Control | technical |
| Safety_PLC | Technical, PLC, SafetyCritical | L1_Control | technical |
| SCADA_Server | Technical, SCADA | L2_Supervisory | technical |
| Historian_OT | Technical, Database | L2_Supervisory | technical |
| Eng_Workstation | Technical, Workstation | L2_Supervisory | technical |
| OT_Switch_Core | Technical, NetworkSwitch | L3_SiteOps | technical |
| Patch_Server | Technical, Server | L3_SiteOps | technical |
| AD_Server_OT | Technical, Server | L3_SiteOps | technical |
| Firewall_ITOT | Technical, Firewall | L3.5_DMZ | technical |
| Jump_Server | Technical, Server | L3.5_DMZ | technical |
| Data_Diode | Technical, SecurityDevice | L3.5_DMZ | technical |
| IT_Switch_Core | Technical, NetworkSwitch | L4_IT | technical |
| Email_Server | Technical, Server | L4_IT | technical |
| ERP_System | Technical, Server | L4_IT | technical |
| AD_Server_IT | Technical, Server | L4_IT | technical |
| VPN_Gateway | Technical, Gateway | L4_IT | technical |

### Human Nodes (7)

| Name | Labels | Role | Access |
|------|--------|------|--------|
| Steve | Human, Operator | Senior OT Technician | OT+IT+Physical+Vendor |
| OT_Manager | Human, Manager | OT Manager | OT+Management |
| IT_Admin | Human, Admin | IT Administrator | IT |
| Control_Op1 | Human, Operator | Control Room Operator | L0-L2 |
| Control_Op2 | Human, Operator | Control Room Operator | L0-L2 |
| Plant_Manager | Human, Manager | Plant Manager | Management |
| Vendor_Rep | Human, Vendor | Vendor Support | Remote+VPN |

### Process Nodes (4)

| Name | Labels | Node Type |
|------|--------|-----------|
| Change_Mgmt_Process | Process, ChangeManagement | process |
| Incident_Response | Process, IncidentResponse | process |
| Vendor_Access_Process | Process, VendorManagement | process |
| Patch_Approval | Process, PatchManagement | process |

---

## MODEL 2: Chemical Facility (24 nodes)

### Technical Nodes (19)

| Name | Labels | Level | Node Type |
|------|--------|-------|-----------|
| SIS_Controller | Technical, SIS, SafetyCritical | Safety | technical |
| SIS_Logic_Solver | Technical, SIS | Safety | technical |
| ESD_Panel | Technical, SIS | Safety | technical |
| DCS_Controller1 | Technical, DCS | DCS | technical |
| DCS_Controller2 | Technical, DCS | DCS | technical |
| DCS_Server | Technical, DCS, Server | DCS | technical |
| Op_Console1 | Technical, Console | DCS | technical |
| Op_Console2 | Technical, Console | DCS | technical |
| OT_Firewall | Technical, Firewall | Site | technical |
| Historian | Technical, Database | Site | technical |
| MES_Server | Technical, Server | Site | technical |
| Eng_Station | Technical, Workstation | Site | technical |
| DMZ_Firewall | Technical, Firewall | DMZ | technical |
| Patch_Relay | Technical, Server | DMZ | technical |
| Remote_Access | Technical, Gateway | DMZ | technical |
| Corp_Firewall | Technical, Firewall | Corporate | technical |
| Corp_Network | Technical, Network | Corporate | technical |
| ERP | Technical, Server | Corporate | technical |
| Internet_GW | Technical, Gateway | Corporate | technical |

### Human Nodes (5)

| Name | Labels | Role | Access |
|------|--------|------|--------|
| DCS_Engineer | Human, Engineer | DCS Engineer | DCS+Site |
| Process_Operator | Human, Operator | Process Operator | DCS |
| Safety_Engineer | Human, Engineer, SafetyCertified | Safety Engineer | Safety+DCS |
| IT_OT_Coord | Human, Coordinator | IT/OT Coordinator | All |
| Site_IT | Human, Admin | Site IT Support | Site+Corp |

---

## MODEL 3a: Water Treatment FLAT (13 nodes)

| Name | Labels | Node Type |
|------|--------|-----------|
| PLC_Chlorine | Technical, PLC | technical |
| PLC_Filtration | Technical, PLC | technical |
| PLC_Pumping | Technical, PLC | technical |
| HMI_1 | Technical, HMI | technical |
| HMI_2 | Technical, HMI | technical |
| SCADA_Server | Technical, SCADA | technical |
| Historian | Technical, Database | technical |
| Switch_A | Technical, NetworkSwitch | technical |
| Switch_B | Technical, NetworkSwitch | technical |
| Switch_C | Technical, NetworkSwitch | technical |
| Eng_Laptop | Technical, Workstation | technical |
| Operator_PC | Technical, Workstation | technical |
| Router_WAN | Technical, Router | technical |

## MODEL 3b: Water Treatment VLAN (14 nodes)

Same as 3a PLUS:

| Name | Labels | Node Type |
|------|--------|-----------|
| L3_Core_Switch | Technical, NetworkSwitch, CoreRouter | technical |
