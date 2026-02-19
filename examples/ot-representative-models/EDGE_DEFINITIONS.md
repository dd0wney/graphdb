# Edge Definitions Reference for cluso-graphdb Implementation
# Extract from representative_models.py (Python/NetworkX validation)
# ALL edges are UNDIRECTED — create in both directions in cluso-graphdb

## MODEL 1: Steve's Utility

### Technical Edges (26 edges)
PLC_Turbine1 <-> HMI_Control1
PLC_Turbine2 <-> HMI_Control2
PLC_Substation <-> HMI_Control1
RTU_Remote1 <-> SCADA_Server
RTU_Remote2 <-> SCADA_Server
Safety_PLC <-> HMI_Control1
Safety_PLC <-> HMI_Control2
HMI_Control1 <-> SCADA_Server
HMI_Control2 <-> SCADA_Server
SCADA_Server <-> Historian_OT
SCADA_Server <-> Eng_Workstation
SCADA_Server <-> OT_Switch_Core
Historian_OT <-> OT_Switch_Core
Eng_Workstation <-> OT_Switch_Core
OT_Switch_Core <-> Patch_Server
OT_Switch_Core <-> AD_Server_OT
OT_Switch_Core <-> Firewall_ITOT
Firewall_ITOT <-> Jump_Server
Firewall_ITOT <-> Data_Diode
Data_Diode <-> Historian_OT
Firewall_ITOT <-> IT_Switch_Core
Jump_Server <-> IT_Switch_Core
IT_Switch_Core <-> Email_Server
IT_Switch_Core <-> ERP_System
IT_Switch_Core <-> AD_Server_IT
IT_Switch_Core <-> VPN_Gateway

### Steve's Access Edges (23 edges)
Steve <-> PLC_Turbine1
Steve <-> PLC_Turbine2
Steve <-> PLC_Substation
Steve <-> HMI_Control1
Steve <-> HMI_Control2
Steve <-> SCADA_Server
Steve <-> Eng_Workstation
Steve <-> Historian_OT
Steve <-> OT_Switch_Core
Steve <-> Patch_Server
Steve <-> Jump_Server
Steve <-> Firewall_ITOT
Steve <-> VPN_Gateway
Steve <-> AD_Server_OT
Steve <-> Change_Mgmt_Process
Steve <-> Incident_Response
Steve <-> Vendor_Access_Process
Steve <-> Patch_Approval
Steve <-> Vendor_Rep
Steve <-> OT_Manager
Steve <-> Control_Op1
Steve <-> Control_Op2
Steve <-> IT_Admin

### Other Human Edges (21 edges)
Control_Op1 <-> HMI_Control1
Control_Op1 <-> HMI_Control2
Control_Op1 <-> Incident_Response
Control_Op2 <-> HMI_Control1
Control_Op2 <-> HMI_Control2
Control_Op2 <-> Incident_Response
OT_Manager <-> SCADA_Server
OT_Manager <-> Change_Mgmt_Process
OT_Manager <-> Patch_Approval
OT_Manager <-> Plant_Manager
IT_Admin <-> IT_Switch_Core
IT_Admin <-> Email_Server
IT_Admin <-> ERP_System
IT_Admin <-> AD_Server_IT
IT_Admin <-> VPN_Gateway
IT_Admin <-> Firewall_ITOT
Plant_Manager <-> ERP_System
Plant_Manager <-> Email_Server
Vendor_Rep <-> VPN_Gateway
Vendor_Rep <-> Jump_Server
Vendor_Rep <-> Vendor_Access_Process

### TOTAL: 70 undirected edges (= 140 directed edges in cluso-graphdb)

---

## MODEL 2: Chemical Facility

### All Edges (37 edges)
SIS_Controller <-> SIS_Logic_Solver
SIS_Logic_Solver <-> ESD_Panel
SIS_Controller <-> DCS_Server
DCS_Controller1 <-> DCS_Server
DCS_Controller2 <-> DCS_Server
DCS_Server <-> Op_Console1
DCS_Server <-> Op_Console2
DCS_Server <-> OT_Firewall
OT_Firewall <-> Historian
OT_Firewall <-> MES_Server
OT_Firewall <-> Eng_Station
OT_Firewall <-> DMZ_Firewall
DMZ_Firewall <-> Patch_Relay
DMZ_Firewall <-> Remote_Access
DMZ_Firewall <-> Corp_Firewall
Corp_Firewall <-> Corp_Network
Corp_Network <-> ERP
Corp_Network <-> Internet_GW
DCS_Engineer <-> Eng_Station
DCS_Engineer <-> DCS_Server
DCS_Engineer <-> DCS_Controller1
DCS_Engineer <-> DCS_Controller2
Process_Operator <-> Op_Console1
Process_Operator <-> Op_Console2
Safety_Engineer <-> SIS_Controller
Safety_Engineer <-> SIS_Logic_Solver
Safety_Engineer <-> DCS_Server
IT_OT_Coord <-> OT_Firewall
IT_OT_Coord <-> DMZ_Firewall
IT_OT_Coord <-> Corp_Firewall
IT_OT_Coord <-> Remote_Access
IT_OT_Coord <-> Patch_Relay
IT_OT_Coord <-> DCS_Engineer
IT_OT_Coord <-> Site_IT
Site_IT <-> Corp_Network
Site_IT <-> Corp_Firewall
Site_IT <-> DMZ_Firewall

### TOTAL: 37 undirected edges (= 74 directed edges in cluso-graphdb)

---

## MODEL 3a: Water Treatment FLAT

### All Edges (13 edges)
Switch_A <-> Switch_B
Switch_B <-> Switch_C
Switch_A <-> Switch_C
PLC_Chlorine <-> Switch_A
PLC_Filtration <-> Switch_A
PLC_Pumping <-> Switch_B
HMI_1 <-> Switch_A
HMI_2 <-> Switch_B
SCADA_Server <-> Switch_B
Historian <-> Switch_C
Eng_Laptop <-> Switch_C
Operator_PC <-> Switch_C
Router_WAN <-> Switch_C

---

## MODEL 3b: Water Treatment VLAN

### All Edges (13 edges)
Switch_A <-> L3_Core_Switch
Switch_B <-> L3_Core_Switch
Switch_C <-> L3_Core_Switch
PLC_Chlorine <-> Switch_A
PLC_Filtration <-> Switch_A
PLC_Pumping <-> Switch_A
HMI_1 <-> Switch_B
HMI_2 <-> Switch_B
SCADA_Server <-> Switch_B
Historian <-> Switch_C
Eng_Laptop <-> Switch_C
Operator_PC <-> Switch_C
Router_WAN <-> L3_Core_Switch
