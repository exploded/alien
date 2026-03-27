#!/bin/bash
# server-setup.sh
#
# One-time setup script to prepare your Linode Debian server for automated
# deployments of the Alien app from GitHub Actions.
#
# Run as root or with sudo:
#   sudo bash scripts/server-setup.sh
#
# After running, follow the printed instructions to add the SSH public key
# to your GitHub repository secrets.

set -e

DEPLOY_USER="deploy"
APP_DIR="/var/www/alien"

echo "=== Alien App - Server Deployment Setup ==="
echo ""

# ---------------------------------------------------------------
# 1. Create deploy user (may already exist from moon setup)
# ---------------------------------------------------------------
if id "$DEPLOY_USER" &>/dev/null; then
    echo "[ok] User '$DEPLOY_USER' already exists"
else
    useradd -m -s /bin/bash "$DEPLOY_USER"
    echo "[ok] Created user '$DEPLOY_USER'"
fi

# ---------------------------------------------------------------
# 2. Generate SSH key pair for GitHub Actions (reuse if exists)
# ---------------------------------------------------------------
KEY_DIR="/home/$DEPLOY_USER/.ssh"
KEY_FILE="$KEY_DIR/github_actions"

mkdir -p "$KEY_DIR"
chmod 700 "$KEY_DIR"

if [ ! -f "$KEY_FILE" ]; then
    ssh-keygen -t ed25519 -f "$KEY_FILE" -N "" -C "github-actions-deploy"
    echo "[ok] Generated SSH key pair at $KEY_FILE"
else
    echo "[ok] SSH key already exists at $KEY_FILE (reusing from previous setup)"
fi

# Authorise the key for the deploy user
if ! grep -qF "$(cat "$KEY_FILE.pub")" "$KEY_DIR/authorized_keys" 2>/dev/null; then
    cat "$KEY_FILE.pub" >> "$KEY_DIR/authorized_keys"
    echo "[ok] Public key added to authorized_keys"
fi

chmod 600 "$KEY_DIR/authorized_keys"
chown -R "$DEPLOY_USER:$DEPLOY_USER" "$KEY_DIR"

# ---------------------------------------------------------------
# 3. Create application directory
# ---------------------------------------------------------------
mkdir -p "$APP_DIR"
echo "[ok] Created $APP_DIR"

# ---------------------------------------------------------------
# 4. Create environment file
# ---------------------------------------------------------------
ENV_FILE="$APP_DIR/.env"

if [ ! -f "$ENV_FILE" ]; then
    cat > "$ENV_FILE" << 'ENV'
PORT=8787
PROD=True
MONITOR_URL=
MONITOR_API_KEY=
ENV
    chmod 600 "$ENV_FILE"
    echo "[ok] Created $ENV_FILE (edit to set MONITOR_URL and MONITOR_API_KEY)"
else
    echo "[ok] $ENV_FILE already exists — not overwriting"
fi

# ---------------------------------------------------------------
# 5. Create systemd service
# ---------------------------------------------------------------
cat > /etc/systemd/system/alien.service << 'SERVICE'
[Unit]
Description=Aliens Like Us Web Application
After=network.target

[Service]
Type=simple
User=www-data
Group=www-data
WorkingDirectory=/var/www/alien
EnvironmentFile=/var/www/alien/.env
ExecStart=/var/www/alien/alien
Restart=always
RestartSec=5

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable alien
echo "[ok] Created and enabled alien.service"

# ---------------------------------------------------------------
# 6. Create the server-side deploy script (runs as root via sudo)
# ---------------------------------------------------------------
cat > /usr/local/bin/deploy-alien << 'DEPLOY_SCRIPT'
#!/bin/bash
# /usr/local/bin/deploy-alien
# Runs as root (via sudo) during GitHub Actions deployments.
#
# To update this script on the server, include scripts/deploy-alien in the SCP
# bundle — it will self-update and re-exec before doing anything else.

set -e

DEPLOY_SRC="${1:-/tmp/alien-deploy}"
DEPLOY_DIR=/var/www/alien

# Self-update: if the bundle contains a newer version of this script, install
# it and re-exec so the rest of the deployment runs with the updated logic.
BUNDLE_SCRIPT="$DEPLOY_SRC/scripts/deploy-alien"
if [ -f "$BUNDLE_SCRIPT" ] && ! diff -q /usr/local/bin/deploy-alien "$BUNDLE_SCRIPT" > /dev/null 2>&1; then
    echo "[deploy] Updating deploy script from bundle..."
    install -m 755 "$BUNDLE_SCRIPT" /usr/local/bin/deploy-alien
    exec /usr/local/bin/deploy-alien "$@"
fi

# Read the service owner from the installed unit — no hardcoded username
SERVICE_USER=$(systemctl show alien --property=User --value)
SERVICE_GROUP=$(systemctl show alien --property=Group --value)

if [ -z "$SERVICE_USER" ]; then
    echo "[deploy] ERROR: Could not read User from alien.service"
    exit 1
fi

echo "[deploy] Stopping service..."
systemctl stop alien || true

echo "[deploy] Installing binary to $DEPLOY_DIR/alien (owner: $SERVICE_USER:$SERVICE_GROUP)..."
rm -f "$DEPLOY_DIR/alien"
cp "$DEPLOY_SRC/alien" "$DEPLOY_DIR/alien"
chmod +x "$DEPLOY_DIR/alien"

echo "[deploy] Updating web assets..."
cp -r "$DEPLOY_SRC/templates/"  "$DEPLOY_DIR/"
cp -r "$DEPLOY_SRC/css/"        "$DEPLOY_DIR/"
cp -r "$DEPLOY_SRC/images/"     "$DEPLOY_DIR/"
cp -r "$DEPLOY_SRC/static/"     "$DEPLOY_DIR/"
cp    "$DEPLOY_SRC/robots.txt"  "$DEPLOY_DIR/"
chown -R "$SERVICE_USER:$SERVICE_GROUP" "$DEPLOY_DIR"

echo "[deploy] Starting service..."
systemctl start alien

echo "[deploy] Verifying service is active..."
sleep 2
if ! systemctl is-active --quiet alien; then
    echo "[deploy] ERROR: Service failed to start. Status:"
    systemctl status alien --no-pager --lines=30
    exit 1
fi

echo "[deploy] Cleaning up..."
rm -rf "$DEPLOY_SRC"

echo "[deploy] Done — alien is running."
DEPLOY_SCRIPT

chmod +x /usr/local/bin/deploy-alien
echo "[ok] Created /usr/local/bin/deploy-alien"

# ---------------------------------------------------------------
# 7. Set ownership of app directory
# ---------------------------------------------------------------
chown -R www-data:www-data "$APP_DIR"
echo "[ok] Set ownership of $APP_DIR to www-data"

# ---------------------------------------------------------------
# 8. Configure sudoers — only allow the one deploy script
# ---------------------------------------------------------------
SUDOERS_FILE="/etc/sudoers.d/alien-deploy"

cat > "$SUDOERS_FILE" << 'EOF'
# Allow the deploy user to run the alien deployment script as root
deploy ALL=(ALL) NOPASSWD: /usr/local/bin/deploy-alien
# Allow stopping the alien service directly (used by the GitHub Actions workflow)
deploy ALL=(ALL) NOPASSWD: /usr/bin/systemctl stop alien
EOF

chmod 440 "$SUDOERS_FILE"
visudo -c -f "$SUDOERS_FILE"
echo "[ok] sudoers entry created at $SUDOERS_FILE"

# ---------------------------------------------------------------
# 9. Print next steps
# ---------------------------------------------------------------
echo ""
echo "=== Setup complete ==="
echo ""
echo "Next steps:"
echo ""
echo "1. Upload your alien.db database to $APP_DIR/:"
echo "   scp alien.db deploy@$(hostname -I | awk '{print $1}'):/tmp/"
echo "   sudo cp /tmp/alien.db $APP_DIR/ && sudo chown www-data:www-data $APP_DIR/alien.db"
echo ""
echo "2. Edit $ENV_FILE to set MONITOR_URL and MONITOR_API_KEY (optional)"
echo ""
echo "3. Add these secrets to your GitHub repository:"
echo "   Go to: GitHub repo → Settings → Secrets and variables → Actions"
echo ""
echo "   Secret name     : DEPLOY_HOST"
echo "   Secret value    : $(hostname -I | awk '{print $1}')  (your server's public IP)"
echo ""
echo "   Secret name     : DEPLOY_USER"
echo "   Secret value    : $DEPLOY_USER"
echo ""
echo "   Secret name     : DEPLOY_SSH_KEY"
echo "   Secret value    : (paste the private key below)"
echo ""
echo "---BEGIN PRIVATE KEY (copy everything including the dashes)---"
cat "$KEY_FILE"
echo "---END PRIVATE KEY---"
echo ""
echo "   Optional secret : DEPLOY_PORT  (only if SSH is not on port 22)"
echo ""
echo "4. Push to main to trigger your first deployment."
