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

Type "# Ultra makes it possible to define your application configuration in one place."
Enter
Sleep 2.5s
Type "bat --paging=never apps/worker/config/config.go"
Enter
Sleep 5s

Type "# First, use `ultra gen` to build a compose override from your Config — offline, no secret store."
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

Type "# Then `ultra run` resolves the secrets from your provider and execs your command with them injected in memory."
Enter
Sleep 2.5s
Type "ultra run apps/worker --secret-resolver env -- sh -c 'printenv | grep ULTRA_WORKER__'"
Enter
Sleep 5s
Type "# Nothing hit disk but the references. The secret values reach only the process you launched."
Enter
Sleep 4s
