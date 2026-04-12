# Google Cloud Console OAuth Setup Guide

This guide covers setting up OAuth credentials for Vibe Kanban's Google Calendar integration.

## Prerequisites

- A Google Account with admin access to create projects
- Google Cloud Console access at https://console.cloud.google.com

---

## Step 1: Create or Select a Google Cloud Project

1. Go to [Google Cloud Console](https://console.cloud.google.com)
2. Click the project dropdown at the top
3. Click **"New Project"** or select an existing project

### If creating a new project:
- **Name**: `Vibe Kanban` (or your preferred name)
- **Organization**: Select your organization if applicable
- **Location**: No organization (or select your org)

Click **Create** and wait for provisioning (~10-30 seconds).

---

## Step 2: Enable Required APIs

1. In the Cloud Console, navigate to **APIs & Services > Library**
2. Search for and enable the following APIs:

| API | Status | Purpose |
|-----|--------|---------|
| Google Identity Toolkit API | Enable | User authentication |
| People API | Enable | Contact/Calendar integration |

3. Click **Enable** for each API

---

## Step 3: Configure OAuth Consent Screen

1. Navigate to **APIs & Services > OAuth consent screen**
2. Choose **External** user type (for production/public use)
3. Click **Create**

### App Information

| Field | Value |
|-------|-------|
| App name | `Vibe Kanban` |
| User support email | Your email |
| App logo | (optional) Upload 192x192px logo |
| Application home page | Your app URL |
| Authorized domains | Your app domain |
| Developer contact | Your email |

### Scopes (Google Calendar)

Click **Add or remove scopes** and add:

```
https://www.googleapis.com/auth/calendar
https://www.googleapis.com/auth/calendar.events
```

### Test Users (for External apps)

Add your email address as a test user during development.

4. Click **Save and Continue** through each section

---

## Step 4: Create OAuth 2.0 Client ID

1. Navigate to **APIs & Services > Credentials**
2. Click **+ Create Credentials > OAuth client ID**
3. Select **Web application** as application type

### Web Application Configuration

| Field | Development Value | Production Value |
|-------|-------------------|------------------|
| Name | `Vibe Kanban Web Client` | `Vibe Kanban Production` |
| Authorized JavaScript origins | `http://localhost:3000` | `https://your-domain.com` |
| Authorized redirect URIs | `http://localhost:3000/auth/callback` | `https://your-domain.com/auth/callback` |

4. Click **Create**

---

## Step 5: Save Your Credentials

After creation, you'll receive a **Client ID** and **Client Secret**. Save these securely:

```bash
# Add to your environment (never commit to git)
export GOOGLE_OAUTH_CLIENT_ID="your-client-id.apps.googleusercontent.com"
export GOOGLE_OAUTH_CLIENT_SECRET="your-client-secret"
```

### ⚠️ Security Notes

- **Never commit** OAuth credentials to version control
- Use environment variables or secret management (e.g., HashiCorp Vault, AWS Secrets Manager)
- Rotate secrets periodically
- Restrict API key usage to specific domains/IPs in production

---

## Step 6: Verify Configuration

### Test the OAuth Flow

1. Visit the OAuth 2.0 Playground: https://developers.google.com/oauthplayground/
2. Enter your Client ID and Secret
3. Select the scopes you configured
4. Click **Authorize** and complete the flow

### Check API Access

```bash
# Test access with a valid token
curl -H "Authorization: Bearer YOUR_ACCESS_TOKEN" \
  https://www.googleapis.com/oauth2/v3/userinfo
```

---

## Environment Variables for Vibe Kanban

Add these to your deployment environment:

```bash
GOOGLE_OAUTH_CLIENT_ID=your-client-id.apps.googleusercontent.com
GOOGLE_OAUTH_CLIENT_SECRET=your-client-secret
GOOGLE_OAUTH_REDIRECT_URI=http://localhost:3000/auth/callback  # dev
# or
GOOGLE_OAUTH_REDIRECT_URI=https://your-domain.com/auth/callback  # prod
```

---

## Troubleshooting

### "redirect_uri_mismatch" Error

- Verify the redirect URI in Google Console **exactly** matches your request (including trailing slashes, http vs https)
- Check both Authorized JavaScript origins AND Authorized redirect URIs

### "access_denied" Error

- Ensure your Google account is added as a Test User (External apps only)
- Verify all required scopes are in the OAuth consent screen

### API Not Enabled

- Return to **APIs & Services > Library** and verify each API shows "API enabled"

---

## Production Checklist

- [ ] Switch OAuth consent screen from "Testing" to "Publishing" status
- [ ] Add all production domains to Authorized JavaScript origins
- [ ] Set up domain verification in Google Search Console
- [ ] Configure Google Cloud billing (if using paid APIs)
- [ ] Set up API key restrictions (IP addresses, referrers)
- [ ] Enable audit logging for OAuth consent decisions
- [ ] Document credential rotation procedures

---

## References

- [Google OAuth 2.0 Documentation](https://developers.google.com/identity/protocols/oauth2)
- [Google Calendar API Documentation](https://developers.google.com/calendar)
- [Setting up OAuth 2.0](https://support.google.com/cloud/answer/6158849)
