# Telecom Model Node Definitions (114 nodes)

## Access_Network (19 nodes)
- Cell_CBD_1 | type=technical | Cell site/base station
- Cell_CBD_2 | type=technical | Cell site/base station
- Cell_CBD_3 | type=technical | Cell site/base station
- Cell_Emergency_HQ | type=technical | Cell site/base station
- Cell_Hospital | type=technical | Cell site/base station
- Cell_Regional_1 | type=technical | Cell site/base station
- Cell_Regional_2 | type=technical | Cell site/base station
- Cell_Suburban_1 | type=technical | Cell site/base station
- Cell_Suburban_2 | type=technical | Cell site/base station
- Exchange_CBD | type=technical | Local exchange
- Exchange_Industrial | type=technical | Local exchange
- Exchange_North | type=technical | Local exchange
- Exchange_South | type=technical | Local exchange
- Exchange_West | type=technical | Local exchange
- FDH_Business_Park | type=technical | Fibre distribution
- FDH_CBD | type=technical | Fibre distribution
- FDH_Data_Centre | type=technical | Fibre distribution
- FDH_Gov_Precinct | type=technical | Fibre distribution
- FDH_Hospital | type=technical | Fibre distribution

## BSS_OSS (9 nodes)
- AAA_Server | type=technical | Business/operations support
- Billing_System | type=technical | Business/operations support
- CRM_System | type=technical | Business/operations support
- DNS_Primary | type=technical | Business/operations support
- DNS_Secondary | type=technical | Business/operations support
- Inventory_System | type=technical | Business/operations support
- Provisioning_System | type=technical | Business/operations support
- RADIUS_Server | type=technical | Business/operations support
- Workforce_Mgmt | type=technical | Business/operations support

## Core_Network (9 nodes)
- Core_Router_ADL | type=technical | MPLS backbone routing
- Core_Router_BNE | type=technical | MPLS backbone routing
- Core_Router_MEL | type=technical | MPLS backbone routing
- Core_Router_PER | type=technical | MPLS backbone routing
- Core_Router_SYD | type=technical | MPLS backbone routing
- DWDM_ADL_PER | type=technical | Optical transport
- DWDM_MEL_ADL | type=technical | Optical transport
- DWDM_SYD_BNE | type=technical | Optical transport
- DWDM_SYD_MEL | type=technical | Optical transport

## Corporate_IT (6 nodes)
- Corp_AD | type=technical | Corporate IT systems
- Corp_ERP | type=technical | Corporate IT systems
- Corp_Email | type=technical | Corporate IT systems
- Corp_Firewall | type=technical | Corporate IT systems
- Corp_VPN | type=technical | Corporate IT systems
- IT_Switch_Core | type=technical | Corporate IT systems

## Human (22 nodes)
- BSS_Admin | type=human | BSS/OSS Administrator
- Banking_Liaison | type=human | Banking Sector Account Manager
- CTO | type=human | Chief Technology Officer
- Emergency_Liaison | type=human | Emergency Services Liaison
- Field_Tech_1 | type=human | Field Technician
- Field_Tech_2 | type=human | Field Technician
- IP_Engineer | type=human | IP/MPLS Engineer
- IT_Admin_Corp | type=human | Corporate IT Admin
- Managed_SOC_Analyst | type=human | Managed SOC Analyst
- Mobile_Engineer | type=human | Mobile Core Engineer
- NOC_Manager | type=human | NOC Manager
- NOC_Operator_1 | type=human | NOC Operator
- NOC_Operator_2 | type=human | NOC Operator
- NOC_Security_Analyst | type=human | Security Analyst
- NOC_Shift_Lead | type=human | NOC Shift Leader
- Network_Director | type=human | Network Operations Director
- Optical_Engineer | type=human | Optical Transport Engineer
- Security_Manager | type=human | Security Manager
- Senior_Network_Eng | type=human | Senior Network Engineer
- Vendor_Cisco | type=human | Cisco TAC Engineer
- Vendor_Ericsson | type=human | Ericsson Support Engineer
- Vendor_Nokia | type=human | Nokia Optical Support

## Interconnection (8 nodes)
- Gateway_Banking | type=technical | Sector interconnection
- Gateway_Emergency | type=technical | Sector interconnection
- Gateway_Energy | type=technical | Sector interconnection
- Gateway_Healthcare | type=technical | Sector interconnection
- Gateway_Internet | type=technical | Sector interconnection
- Gateway_Transport | type=technical | Sector interconnection
- IX_Peering | type=technical | Sector interconnection
- SIP_Trunk_Emergency | type=technical | Sector interconnection

## Mobile_Core (8 nodes)
- HSS_Primary | type=technical | Mobile network core
- IMS_Core | type=technical | Mobile network core
- MME_Primary | type=technical | Mobile network core
- MME_Secondary | type=technical | Mobile network core
- PGW_Primary | type=technical | Mobile network core
- PGW_Secondary | type=technical | Mobile network core
- SGW_Primary | type=technical | Mobile network core
- SGW_Secondary | type=technical | Mobile network core

## NOC (7 nodes)
- Fault_Mgmt_System | type=technical | Network operations
- NMS_Primary | type=technical | Network operations
- NMS_Secondary | type=technical | Network operations
- NOC_Dashboard | type=technical | Network operations
- Performance_Monitor | type=technical | Network operations
- SIEM_Telecom | type=technical | Network operations
- Ticketing_System | type=technical | Network operations

## Process (8 nodes)
- Capacity_Planning | type=process | Capacity planning process
- Change_Advisory_Board | type=process | Change advisory board
- Disaster_Recovery | type=process | Disaster recovery process
- Incident_Mgmt_Process | type=process | Incident management process
- Patch_Management | type=process | Patch management process
- Regulatory_Compliance | type=process | ACMA/critical infrastructure obligations
- SLA_Management | type=process | Service level agreement monitoring
- Vendor_Access_Control | type=process | Vendor remote access governance

## Sector_Banking (4 nodes)
- Bank_ATM_Network | type=external | Banking infrastructure
- Bank_Branch_WAN | type=external | Banking infrastructure
- Bank_SWIFT_Gateway | type=external | Banking infrastructure
- Bank_Trading_Floor | type=external | Banking infrastructure

## Sector_Emergency (5 nodes)
- Ambulance_Dispatch | type=external | Emergency services
- CAD_System | type=external | Emergency services
- Fire_Dispatch | type=external | Emergency services
- Police_Radio_GW | type=external | Emergency services
- Triple_Zero_Centre | type=external | Emergency services

## Sector_Energy (3 nodes)
- Gas_Pipeline_Comms | type=external | Energy infrastructure
- Grid_SCADA_Comms | type=external | Energy infrastructure
- Substation_Comms | type=external | Energy infrastructure

## Sector_Healthcare (3 nodes)
- Hospital_Network | type=external | Healthcare infrastructure
- Pathology_WAN | type=external | Healthcare infrastructure
- Telehealth_Platform | type=external | Healthcare infrastructure

## Sector_Transport (3 nodes)
- Port_Operations | type=external | Transport infrastructure
- Rail_SCADA_Comms | type=external | Transport infrastructure
- Traffic_Mgmt_System | type=external | Transport infrastructure
