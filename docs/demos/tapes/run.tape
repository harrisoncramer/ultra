Output ../assets/demos/run.gif

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 700
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set Padding 24

Hide
Type "export PATH=$PWD:$PATH"
Enter
Type "unset DATABASE_URL STRIPE_SECRET_KEY DEV_DATABASE_URL LOG_LEVEL"
Enter
Type "export DATABASE_URL=postgres://user:pass@db:5432/app"
Enter
Type "export STRIPE_SECRET_KEY=sk_live_abc123"
Enter
Type "rm -rf tmp"
Enter
Type "clear"
Enter
Show

Type "# run resolves the secrets and execs your command with them injected in memory"
Enter
Sleep 500ms
Type "ultra run apps/worker --secret-resolver env -- sh -c 'printenv | grep ULTRA_WORKER__ | sort'"
Enter
Sleep 3s
Type "# The only artifact on disk is the override — references, never the secret values"
Enter
Sleep 500ms
Type "bat --paging=never tmp/ultra.compose.yml"
Enter
Sleep 4s
