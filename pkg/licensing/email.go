package licensing

import (
	"bytes"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
)

// EmailConfig holds email configuration
type EmailConfig struct {
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	FromEmail    string
	FromName     string
}

// LoadEmailConfigFromEnv loads email configuration from environment variables
func LoadEmailConfigFromEnv() *EmailConfig {
	return &EmailConfig{
		SMTPHost:     os.Getenv("SMTP_HOST"),     // e.g., smtp.sendgrid.net, smtp.mailgun.org
		SMTPPort:     os.Getenv("SMTP_PORT"),     // usually 587
		SMTPUsername: os.Getenv("SMTP_USERNAME"), // API key for SendGrid, username for others
		SMTPPassword: os.Getenv("SMTP_PASSWORD"), // API key or password
		FromEmail:    os.Getenv("FROM_EMAIL"),    // e.g., licenses@graphdb.dev
		FromName:     os.Getenv("FROM_NAME"),     // e.g., GraphDB Licensing
	}
}

// IsConfigured checks if email is properly configured
func (c *EmailConfig) IsConfigured() bool {
	return c.SMTPHost != "" && c.SMTPPort != "" && c.SMTPUsername != "" && c.SMTPPassword != "" && c.FromEmail != ""
}

// SendLicenseEmail sends a license key to a customer
func SendLicenseEmail(config *EmailConfig, license *License) error {
	if !config.IsConfigured() {
		return fmt.Errorf("email not configured (set SMTP_HOST, SMTP_PORT, SMTP_USERNAME, SMTP_PASSWORD, FROM_EMAIL)")
	}

	// Prepare email content
	subject := fmt.Sprintf("Your GraphDB %s License", license.Type)
	body, err := generateLicenseEmailHTML(license)
	if err != nil {
		return fmt.Errorf("failed to generate email: %w", err)
	}

	// Build email message
	from := config.FromEmail
	if config.FromName != "" {
		from = fmt.Sprintf("%s <%s>", config.FromName, config.FromEmail)
	}

	headers := make(map[string]string)
	headers["From"] = from
	headers["To"] = license.Email
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=UTF-8"

	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + body

	// Send via SMTP
	auth := smtp.PlainAuth("", config.SMTPUsername, config.SMTPPassword, config.SMTPHost)
	addr := fmt.Sprintf("%s:%s", config.SMTPHost, config.SMTPPort)

	err = smtp.SendMail(addr, auth, config.FromEmail, []string{license.Email}, []byte(message))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// generateLicenseEmailHTML creates HTML email content for a license
func generateLicenseEmailHTML(license *License) (string, error) {
	tmpl := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            line-height: 1.6;
            color: #333;
            max-width: 600px;
            margin: 0 auto;
            padding: 20px;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 30px;
            border-radius: 8px 8px 0 0;
            text-align: center;
        }
        .content {
            background: #f9f9f9;
            padding: 30px;
            border-radius: 0 0 8px 8px;
        }
        .license-key {
            background: #fff;
            border: 2px solid #667eea;
            border-radius: 8px;
            padding: 20px;
            margin: 20px 0;
            font-family: 'Courier New', monospace;
            font-size: 24px;
            font-weight: bold;
            text-align: center;
            color: #667eea;
            letter-spacing: 2px;
        }
        .info {
            background: #fff;
            border-left: 4px solid #667eea;
            padding: 15px;
            margin: 20px 0;
        }
        .footer {
            margin-top: 30px;
            padding-top: 20px;
            border-top: 1px solid #ddd;
            font-size: 12px;
            color: #666;
            text-align: center;
        }
        .button {
            display: inline-block;
            background: #667eea;
            color: white;
            padding: 12px 30px;
            text-decoration: none;
            border-radius: 6px;
            margin: 20px 0;
        }
        code {
            background: #f4f4f4;
            padding: 2px 6px;
            border-radius: 3px;
            font-family: 'Courier New', monospace;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>🎉 Welcome to GraphDB {{.Type}}</h1>
        <p>Your license has been activated!</p>
    </div>

    <div class="content">
        <p>Hi there,</p>
        <p>Thank you for purchasing GraphDB {{.Type}} Edition! Your license has been generated and is ready to use.</p>

        <div class="license-key">
            {{.Key}}
        </div>

        <div class="info">
            <strong>📋 License Details:</strong><br>
            <strong>Edition:</strong> {{.Type}}<br>
            <strong>Email:</strong> {{.Email}}<br>
            <strong>Status:</strong> {{.Status}}<br>
            <strong>Created:</strong> {{.CreatedAt.Format "January 2, 2006"}}
        </div>

        <h3>🚀 Getting Started</h3>
        <p>To activate your Enterprise license, set the environment variable:</p>
        <code>GRAPHDB_LICENSE_KEY={{.Key}}</code>

        <p>Or save your license to a file:</p>
        <code>echo "{{.Key}}" > /etc/graphdb/license.key</code>

        <p>Start your GraphDB server:</p>
        <code>GRAPHDB_EDITION=enterprise ./bin/server</code>

        <div class="info">
            <strong>📚 Resources:</strong><br>
            • <a href="https://docs.graphdb.dev">Documentation</a><br>
            • <a href="https://docs.graphdb.dev/deployment">Deployment Guide</a><br>
            • <a href="https://docs.graphdb.dev/api">API Reference</a><br>
            • <a href="mailto:support@graphdb.dev">Support Email</a>
        </div>

        <p>If you have any questions or need assistance, please don't hesitate to reach out to our support team.</p>

        <p>Thank you for choosing GraphDB!</p>

        <p>Best regards,<br>
        <strong>The GraphDB Team</strong></p>
    </div>

    <div class="footer">
        <p>This email was sent to {{.Email}} because you purchased a GraphDB license.</p>
        <p>GraphDB • Enterprise Graph Database</p>
    </div>
</body>
</html>`

	t, err := template.New("license").Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, license); err != nil {
		return "", err
	}

	return buf.String(), nil
}
