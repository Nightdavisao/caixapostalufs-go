# caixapostalufs-go
> [!WARNING]
> the code is slop (specifically the IMAP server part). i might or might not clean shit up later

a go "library" (more like just some shit thrown together) for the reverse-engineered HTTP API from the Caixa Postal mobile app from the Universidade Federal de Sergipe, with a sloppy barebones IMAP server implemented under `internal/mail`

the reverse-engineering part probably couldn't be possible without [Reflutter](https://github.com/Impact-I/reFlutter). thank you, Reflutter! i also want to thank my jailbroken iPhone 8 Plus for letting me use it as the guinea pig for testing every functionality of the app so that the requests would show up on my http proxy. :)