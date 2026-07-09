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
Type "export DATABASE_URL=postgres://user:pass@db:5432/app"
Enter
Type "export STRIPE_SECRET_KEY=sk_live_abc123"
Enter
Type "rm -rf tmp"
Enter
Type "clear"
Enter
Show

Type "# First, define your application configuration."
Enter
Sleep 2.5s
Type "bat apps/worker/config/config.go"
Enter
Sleep 5s

Type "# Next, use `ultra gen` to build a Docker compose override."
Enter
Sleep 2.5s
Type "ultra gen apps/worker"
Enter
Sleep 3.5s
Type "# The override maps each secret to a launcher variable."
Enter
Sleep 2.5s
Type "bat tmp/ultra.compose.yml"
Enter
Sleep 5s

Type "# Next, use `ultra run` to resolve secrets from your provider (like 1password, aws, etc) into your service, purely in memory."
Enter
Sleep 2.5s
Type "ultra run apps/worker --secret-resolver env -- sh -c 'printenv | grep ULTRA_WORKER__'"
Enter
Sleep 5s
Type "# Nothing was written to disk, and drift is impossible. The config package is your source of truth, and values reach only the process you launched."
Enter
Sleep 4s
