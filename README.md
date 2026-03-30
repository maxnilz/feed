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
    filter:
      type: "embedding" # "embedding", "gemini" or "openai", defaults to "embedding"
      model: "" # Optional model name for "gemini" or "openai"; defaults to provider default when empty
      geminiApiKey: "your-gemini-api-key" # Gemini API for semantic filtering
      openaiApiKey: your-openai-key
    fetchInterval: 10m # How often to fetch new items (e.g., 10m, 1h). Default 10 minutes.
    fetchTimeout: 30s # HTTP timeout for fetching feeds. Default 30 seconds.
    subscribers:
      - name: foo
        email: foo@example.com
        # follow the spec in https://en.wikipedia.org/wiki/Cron, in which it
        # requires 5 entries: minute, hour, day of month, month and day of week.
        # you can find examples from there https://crontab.guru/
        schedule: '* * * * *'
        sources:
          - name: Evan Jones
            url: https://www.evanjones.ca/index.rss
            semanticFilters:
              - "distributed systems"
              - "database internals"
            similarityThreshold: 0.7
          - name: HN Tech
            urls:
              - https://hnrss.org/active
              - https://hnrss.org/newest?q=database
            semanticFiltersFile: "prompts/hn-filters.txt"
            llmPromptFile: "prompts/hn-prompt.md"
            similarityThreshold: 0.7
          - name: HN Embedded
            url: https://hnrss.org/newest
            embeddedFiltersFile: "hn-filters.txt"
            embeddedPromptFile: "hn-prompt.md"
            similarityThreshold: 0.8
      - name: bar
        email: bar@example.com
        schedule: '* * * * *'
        sources:
          - name: Evan Jones
            url: https://www.evanjones.ca/index.rss
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
- [ ] CI/CD integration
