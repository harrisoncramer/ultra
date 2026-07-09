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

Type "# The `gen` command builds the compose override from your Config. Here's our config package:"
Enter
Sleep 2.5s
Type "bat apps/worker/config/config.go"
Enter
Sleep 5s
Type "# Let's generate our binding file first."
Enter
Sleep 2.5s
Type "ultra gen apps/worker"
Enter
Sleep 3.5s
Type "# Now, the override maps each secret to a launcher variable"
Enter
Sleep 2.5s
Type "bat tmp/ultra.compose.yml"
Enter
Sleep 6s
Type "# This binding file can now inject secrets via Ultra to the running containers"
Enter
Sleep 2.5s
