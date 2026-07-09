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

Type "# The `run` command resolves the secrets from your resolver into the environment."
Enter
Sleep 2.5s
Type "ultra run apps/worker --secret-resolver env -- sh -c 'printenv | grep ULTRA_WORKER__'"
Enter
Sleep 5s
Type "# It regenerates the compose override from your Config each run, so secrets reach Docker with nothing written to disk."
Enter
Sleep 2.5s
