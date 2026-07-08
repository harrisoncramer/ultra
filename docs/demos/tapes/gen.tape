Output ../assets/demos/gen.gif

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 700
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms
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

Type "# gen builds the compose override from your Config — offline, no secret store"
Enter
Sleep 2.5s
Type "bat --paging=never apps/worker/config/config.go"
Enter
Sleep 5s
Type "ultra gen apps/worker"
Enter
Sleep 3.5s
Type "# The override maps each secret to a launcher variable — references only, never a value"
Enter
Sleep 2.5s
Type "bat --paging=never tmp/ultra.compose.yml"
Enter
Sleep 6s
