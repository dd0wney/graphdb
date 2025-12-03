package compliance

// Framework-specific control initialization

// initializeGDPRControls initializes GDPR compliance controls
func (c *ComplianceChecker) initializeGDPRControls() {
	c.controls[FrameworkGDPR] = []Control{
		{
			ID:          "GDPR-ART32-ENCRYPT",
			Framework:   FrameworkGDPR,
			Title:       "Article 32: Encryption of Personal Data",
			Description: "Implement appropriate encryption measures to ensure data security",
			Status:      StatusPartial,
		},
		{
			ID:          "GDPR-ART32-PSEUDONYMIZE",
			Framework:   FrameworkGDPR,
			Title:       "Article 32: Pseudonymisation",
			Description: "Implement pseudonymisation and data masking techniques",
			Status:      StatusPartial,
		},
		{
			ID:          "GDPR-ART30-LOGS",
			Framework:   FrameworkGDPR,
			Title:       "Article 30: Records of Processing Activities",
			Description: "Maintain comprehensive audit logs of data processing",
			Status:      StatusPartial,
		},
		{
			ID:          "GDPR-ART32-ACCESS",
			Framework:   FrameworkGDPR,
			Title:       "Article 32: Access Control",
			Description: "Implement proper authentication and access control mechanisms",
			Status:      StatusPartial,
		},
		{
			ID:          "GDPR-ART25-PRIVACY",
			Framework:   FrameworkGDPR,
			Title:       "Article 25: Privacy by Design",
			Description: "Implement privacy controls throughout the system",
			Status:      StatusPartial,
		},
	}
}

// initializeSOC2Controls initializes SOC 2 compliance controls
func (c *ComplianceChecker) initializeSOC2Controls() {
	c.controls[FrameworkSOC2] = []Control{
		{
			ID:          "SOC2-CC6.1-ENCRYPT",
			Framework:   FrameworkSOC2,
			Title:       "CC6.1: Logical and Physical Access Controls - Encryption",
			Description: "Use encryption to protect data at rest and in transit",
			Status:      StatusPartial,
		},
		{
			ID:          "SOC2-CC6.7-CRYPTO",
			Framework:   FrameworkSOC2,
			Title:       "CC6.7: Transmission Security",
			Description: "Protect data transmission with TLS/SSL encryption",
			Status:      StatusPartial,
		},
		{
			ID:          "SOC2-CC7.2-MONITOR",
			Framework:   FrameworkSOC2,
			Title:       "CC7.2: System Monitoring",
			Description: "Implement continuous monitoring and audit logging",
			Status:      StatusPartial,
		},
		{
			ID:          "SOC2-CC6.2-AUTH",
			Framework:   FrameworkSOC2,
			Title:       "CC6.2: Authentication and Authorization",
			Description: "Implement strong authentication mechanisms",
			Status:      StatusPartial,
		},
		{
			ID:          "SOC2-CC6.6-KEY",
			Framework:   FrameworkSOC2,
			Title:       "CC6.6: Encryption Key Management",
			Description: "Proper management of cryptographic keys",
			Status:      StatusPartial,
		},
	}
}

// initializeHIPAAControls initializes HIPAA compliance controls
func (c *ComplianceChecker) initializeHIPAAControls() {
	c.controls[FrameworkHIPAA] = []Control{
		{
			ID:          "HIPAA-164.312(a)(2)(iv)-ENCRYPT",
			Framework:   FrameworkHIPAA,
			Title:       "164.312(a)(2)(iv): Encryption and Decryption",
			Description: "Implement encryption for electronic protected health information (ePHI)",
			Status:      StatusPartial,
		},
		{
			ID:          "HIPAA-164.312(b)-AUDIT",
			Framework:   FrameworkHIPAA,
			Title:       "164.312(b): Audit Controls",
			Description: "Implement hardware, software, and procedural mechanisms to record and examine access",
			Status:      StatusPartial,
		},
		{
			ID:          "HIPAA-164.312(a)(1)-ACCESS",
			Framework:   FrameworkHIPAA,
			Title:       "164.312(a)(1): Access Control",
			Description: "Implement technical policies and procedures for access authorization",
			Status:      StatusPartial,
		},
		{
			ID:          "HIPAA-164.312(e)(1)-TRANSPORT",
			Framework:   FrameworkHIPAA,
			Title:       "164.312(e)(1): Transmission Security",
			Description: "Implement technical security measures to guard against unauthorized access during transmission",
			Status:      StatusPartial,
		},
		{
			ID:          "HIPAA-164.308(a)(1)(ii)(D)-MASK",
			Framework:   FrameworkHIPAA,
			Title:       "164.308(a)(1)(ii)(D): Information System Activity Review",
			Description: "Implement procedures to regularly review logs and protect PHI disclosure",
			Status:      StatusPartial,
		},
	}
}

// initializePCIDSSControls initializes PCI-DSS compliance controls
func (c *ComplianceChecker) initializePCIDSSControls() {
	c.controls[FrameworkPCIDSS] = []Control{
		{
			ID:          "PCIDSS-3.4-ENCRYPT",
			Framework:   FrameworkPCIDSS,
			Title:       "Requirement 3.4: Render PAN Unreadable",
			Description: "Use strong cryptography and security protocols to safeguard sensitive cardholder data",
			Status:      StatusPartial,
		},
		{
			ID:          "PCIDSS-4.1-TRANSPORT",
			Framework:   FrameworkPCIDSS,
			Title:       "Requirement 4.1: Strong Cryptography for Transmission",
			Description: "Use strong cryptography and security protocols for transmitting cardholder data",
			Status:      StatusPartial,
		},
		{
			ID:          "PCIDSS-10.1-AUDIT",
			Framework:   FrameworkPCIDSS,
			Title:       "Requirement 10.1: Audit Trail",
			Description: "Implement audit trails to link all access to system components",
			Status:      StatusPartial,
		},
		{
			ID:          "PCIDSS-3.5.1-KEY",
			Framework:   FrameworkPCIDSS,
			Title:       "Requirement 3.5.1: Key Management",
			Description: "Restrict access to cryptographic keys to the fewest number of custodians necessary",
			Status:      StatusPartial,
		},
		{
			ID:          "PCIDSS-8.2-AUTH",
			Framework:   FrameworkPCIDSS,
			Title:       "Requirement 8.2: User Authentication",
			Description: "Implement proper authentication and access control",
			Status:      StatusPartial,
		},
		{
			ID:          "PCIDSS-3.3-MASK",
			Framework:   FrameworkPCIDSS,
			Title:       "Requirement 3.3: Mask PAN When Displayed",
			Description: "Mask PAN when displayed (show first six and last four digits at most)",
			Status:      StatusPartial,
		},
	}
}

// initializeFIPS1402Controls initializes FIPS 140-2 compliance controls
func (c *ComplianceChecker) initializeFIPS1402Controls() {
	c.controls[FrameworkFIPS1402] = []Control{
		{
			ID:          "FIPS140-2-CRYPTO",
			Framework:   FrameworkFIPS1402,
			Title:       "Cryptographic Module Specification",
			Description: "Use FIPS 140-2 approved cryptographic algorithms (AES, SHA-256)",
			Status:      StatusPartial,
		},
		{
			ID:          "FIPS140-2-KEY",
			Framework:   FrameworkFIPS1402,
			Title:       "Cryptographic Key Management",
			Description: "Implement secure key generation, distribution, storage, and destruction",
			Status:      StatusPartial,
		},
		{
			ID:          "FIPS140-2-ACCESS",
			Framework:   FrameworkFIPS1402,
			Title:       "Physical Security and Access Control",
			Description: "Implement role-based access control and authentication",
			Status:      StatusPartial,
		},
	}
}

// initializeISO27001Controls initializes ISO 27001 compliance controls
func (c *ComplianceChecker) initializeISO27001Controls() {
	c.controls[FrameworkISO27001] = []Control{
		{
			ID:          "ISO27001-A.10.1.1-CRYPTO",
			Framework:   FrameworkISO27001,
			Title:       "A.10.1.1: Policy on the Use of Cryptographic Controls",
			Description: "Develop and implement a policy on the use of cryptographic controls",
			Status:      StatusPartial,
		},
		{
			ID:          "ISO27001-A.10.1.2-KEY",
			Framework:   FrameworkISO27001,
			Title:       "A.10.1.2: Key Management",
			Description: "Implement policy and procedures for key management lifecycle",
			Status:      StatusPartial,
		},
		{
			ID:          "ISO27001-A.12.4.1-LOGS",
			Framework:   FrameworkISO27001,
			Title:       "A.12.4.1: Event Logging",
			Description: "Maintain event logs recording user activities, exceptions, and security events",
			Status:      StatusPartial,
		},
		{
			ID:          "ISO27001-A.9.4.1-ACCESS",
			Framework:   FrameworkISO27001,
			Title:       "A.9.4.1: Information Access Restriction",
			Description: "Restrict access to information and application system functions",
			Status:      StatusPartial,
		},
		{
			ID:          "ISO27001-A.13.1.1-TRANSPORT",
			Framework:   FrameworkISO27001,
			Title:       "A.13.1.1: Network Controls",
			Description: "Implement network security controls and encryption for data in transit",
			Status:      StatusPartial,
		},
	}
}
