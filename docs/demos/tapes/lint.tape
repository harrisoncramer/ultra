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

Type "# The `lint` command compares the keys our config requires against the keys the resolver offers"
Enter
Sleep 2.5s
Type "# It never reads their values -- it's "weaker" than validate -- but can run in less-permisisoned environments."
Enter
Sleep 2.5s
Type "ultra lint apps/worker --secret-resolver env"
Enter
Sleep 4.5s
Type "# To see it working, try removing a required key, and see lint catch the drift"
Enter
Sleep 2.5s
Type "unset STRIPE_SECRET_KEY"
Enter
Type "ultra lint apps/worker --secret-resolver env"
Enter
Sleep 6s
