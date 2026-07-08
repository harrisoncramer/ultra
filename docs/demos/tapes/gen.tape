Output ../assets/demos/gen.gif

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

Type "# gen builds the compose override from your Config — offline, no secret store"
Enter
Sleep 500ms
Type "bat --paging=never apps/worker/config/config.go"
Enter
Sleep 3s
Type "ultra gen apps/worker"
Enter
Sleep 1500ms
Type "# The override maps each secret to a launcher variable — references only, never a value"
Enter
Sleep 500ms
Type "bat --paging=never tmp/ultra.compose.yml"
Enter
Sleep 4s
