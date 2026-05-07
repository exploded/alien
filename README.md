# Aliens Like Us

A little corner of the internet for thinking about extraterrestrial life.

## What it is

Imagine you've been sent to a planet in a far away galaxy. There's intelligent life there. What do you think they look like? What is their society like?

**Aliens Like Us** asks you 59 yes/no questions about hypothetical alien life — biology, behaviour, culture, civilisation — and after each answer shows you what everyone else thinks. It's part thought experiment, part opinion poll, part conversation starter.

Live at **[alienslikeus.com](https://alienslikeus.com)**.

## How it works (briefly)

- Answer a question.
- See the running tally of yes/no votes from everyone who's answered before you.
- Move on to the next question.
- Repeat until you've made it through all 59.

No accounts, no tracking, no ads. Just questions.

## Running it yourself

```bash
go build -o alien.exe .
./alien.exe
```

Listens on port `8787`. The SQLite database is created automatically on first run.

## Credits

- Background images thanks to [Pixabay](https://pixabay.com/).
- Built with 100% pure [Go](https://go.dev/).
