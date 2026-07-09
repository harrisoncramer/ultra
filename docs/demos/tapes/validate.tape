Output ../assets/demos/validate.gif

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
Type "rm -rf tmp"
Enter
Type "clear"
Enter
Show

Type "# The `validate` command resolves secrets from the secret resolver and checks the app's config actually parses"
Enter
Sleep 2.5s
Type "export DATABASE_URL=postgres://user:pass@db:5432/app"
Enter
Type "export STRIPE_SECRET_KEY=sk_live_abc123"
Enter
Type "ultra validate apps/worker --secret-resolver env"
Enter
Sleep 4.5s
Type "# Drop a required secret, and validate will fail before the container ever starts"
Enter
Sleep 2.5s
Type "unset STRIPE_SECRET_KEY"
Enter
Type "ultra validate apps/worker --secret-resolver env"
Enter
Sleep 6s
