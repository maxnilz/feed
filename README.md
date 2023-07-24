# feed

A tool for fetching network rss & send notification over email. Support multiple subscribers with multiple rss sources.

## How to use

- Install from source code
    ```bash
    $ go install github.com/maxnilz/feed
    ```
- Start it from command line
    ```bash
    $ feed -config path/to/config.yaml -verbose
    ```

- Here is a sample config file, update it properly.
    ```yaml
    dsn: "sqlite3:///abs/path/to/feed.db"
    subscribers:
      - name: foo
        email: foo@example.com
        sites:
          - name: Evan Jones
            url: https://www.evanjones.ca/index.rss
        # follow the spec in https://en.wikipedia.org/wiki/Cron, in which it 
        # requires 5 entries: minute, hour, day of month, month and day of week.
        # you can find examples from there https://crontab.guru/
        schedule: '* * * * *'
      - name: bar
        email: bar@example.com
        sites:
          - name: Evan Jones
            url: https://www.evanjones.ca/index.rss
        schedule: '* * * * *'
    mailSender:
      smtpServer: smtp.example.com:587
      senderAddr: sender@example.com
      password: password of sender email
    ```
- Or you can run it via docker
    ```bash
    $ docker run --rm -v ${PWD}/config.yaml:/usr/local/feed/config.yaml -v ${PWD}/feed.db:/usr/local/feed/feed.db --name feed maxnilz/feed:0.2.0
    ```
  Please note that the mounted `feed.db` path needs to match the path to the `dsn` in the config file.

## TODOs

- [ ] Dependency injection
- [ ] Support dynamic schedule
- [ ] Support SMTP server behind proxy
- [ ] Improve performance
- [ ] Support config watch & reload
- [ ] More config formats, e.g., env vars, dotenv, etc.
- [ ] CI/CD integration
