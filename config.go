package main

type Config struct {
	DSN         string       `yaml:"dsn"`
	Subscribers []Subscriber `yaml:"subscribers"`
	MailSender  MailSender   `yaml:"mailSender"`
}

type Subscriber struct {
	Name     string `yaml:"name"`
	Email    string `yaml:"email"`
	Sites    []Site `yaml:"sites"`
	Schedule string `yaml:"schedule"`
}

type Site struct {
	Name string   `yaml:"name"`
	URL  string   `yaml:"url"`
	URLs []string `yaml:"urls"`
}

type MailSender struct {
	SmtpServer string `yaml:"smtpServer"`
	SenderAddr string `yaml:"senderAddr"`
	Password   string `yaml:"password"`
}
