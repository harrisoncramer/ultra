Output ../assets/demos/lint.gif

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

Type "# lint compares the keys Config requires against the keys the resolver offers"
Enter
Sleep 500ms
Type "# It never reads a value, so it runs where the real secrets aren't reachable"
Enter
Sleep 500ms
Type "ultra lint apps/worker --secret-resolver env"
Enter
Sleep 2500ms
Type "# Remove a required key and lint catches the drift"
Enter
Sleep 500ms
Type "unset STRIPE_SECRET_KEY"
Enter
Type "ultra lint apps/worker --secret-resolver env"
Enter
Sleep 4s
