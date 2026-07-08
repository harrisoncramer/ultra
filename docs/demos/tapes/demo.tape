Output ../assets/demos/demo.gif

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 760
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set Padding 24

Hide
Type "export PATH=$PWD:$PATH"
Enter
Type "unset DATABASE_URL STRIPE_SECRET_KEY DEV_DATABASE_URL LOG_LEVEL"
Enter
Type "rm -rf tmp"
Enter
Type "clear"
Enter
Show

Type "# One Go struct is the whole config contract"
Enter
Sleep 2.5s
Type "bat --paging=never apps/worker/config/config.go"
Enter
Sleep 5s

Type "# ultra gen: names-only compose override, no secret store needed"
Enter
Sleep 2.5s
Type "ultra gen apps/worker"
Enter
Sleep 3.5s
Type "bat --paging=never tmp/ultra.compose.yml"
Enter
Sleep 5s

Type "# Provide the required secrets, then validate the app boots clean"
Enter
Sleep 2.5s
Type "export DATABASE_URL=postgres://user:pass@db:5432/app"
Enter
Type "export STRIPE_SECRET_KEY=sk_live_abc123"
Enter
Type "ultra validate apps/worker --secret-resolver env"
Enter
Sleep 4.5s

Type "# Drop a required secret and it fails before the container ever starts"
Enter
Sleep 2.5s
Type "unset STRIPE_SECRET_KEY"
Enter
Type "ultra validate apps/worker --secret-resolver env"
Enter
Sleep 5.5s
