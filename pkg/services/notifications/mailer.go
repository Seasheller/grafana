// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package notifications

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"net"
	"strconv"

	"github.com/Seasheller/grafana/pkg/models"
	"github.com/Seasheller/grafana/pkg/setting"
	"github.com/Seasheller/grafana/pkg/util/errutil"
	gomail "gopkg.in/mail.v2"
)

func (ns *NotificationService) send(msg *Message) (int, error) {
	dialer, err := ns.createDialer()
	if err != nil {
		return 0, err
	}

	var num int
	for _, address := range msg.To {
		m := gomail.NewMessage()
		m.SetHeader("From", msg.From)
		m.SetHeader("To", address)
		m.SetHeader("Subject", msg.Subject)
		for _, file := range msg.EmbededFiles {
			m.Embed(file)
		}

		m.SetBody("text/html", msg.Body)

		e := dialer.DialAndSend(m)
		if e != nil {
			err = errutil.Wrapf(e, "Failed to send notification to email address: %s", address)
			continue
		}

		num++
	}

	return num, err
}

func (ns *NotificationService) createDialer() (*gomail.Dialer, error) {
	host, port, err := net.SplitHostPort(ns.Cfg.Smtp.Host)

	if err != nil {
		return nil, err
	}
	iPort, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}

	tlsconfig := &tls.Config{
		InsecureSkipVerify: ns.Cfg.Smtp.SkipVerify,
		ServerName:         host,
	}

	if ns.Cfg.Smtp.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(ns.Cfg.Smtp.CertFile, ns.Cfg.Smtp.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("Could not load cert or key file. error: %v", err)
		}
		tlsconfig.Certificates = []tls.Certificate{cert}
	}

	d := gomail.NewDialer(host, iPort, ns.Cfg.Smtp.User, ns.Cfg.Smtp.Password)
	d.TLSConfig = tlsconfig

	if ns.Cfg.Smtp.EhloIdentity != "" {
		d.LocalName = ns.Cfg.Smtp.EhloIdentity
	} else {
		d.LocalName = setting.InstanceName
	}
	return d, nil
}

func (ns *NotificationService) buildEmailMessage(cmd *models.SendEmailCommand) (*Message, error) {
	if !ns.Cfg.Smtp.Enabled {
		return nil, models.ErrSmtpNotEnabled
	}

	var buffer bytes.Buffer
	var err error

	data := cmd.Data
	if data == nil {
		data = make(map[string]interface{}, 10)
	}

	setDefaultTemplateData(data, nil)
	err = mailTemplates.ExecuteTemplate(&buffer, cmd.Template, data)
	if err != nil {
		return nil, err
	}

	subject := cmd.Subject
	if cmd.Subject == "" {
		var subjectText interface{}
		subjectData := data["Subject"].(map[string]interface{})
		subjectText, hasSubject := subjectData["value"]

		if !hasSubject {
			return nil, fmt.Errorf("Missing subject in Template %s", cmd.Template)
		}

		subjectTmpl, err := template.New("subject").Parse(subjectText.(string))
		if err != nil {
			return nil, err
		}

		var subjectBuffer bytes.Buffer
		err = subjectTmpl.ExecuteTemplate(&subjectBuffer, "subject", data)
		if err != nil {
			return nil, err
		}

		subject = subjectBuffer.String()
	}

	return &Message{
		To:           cmd.To,
		From:         fmt.Sprintf("%s <%s>", ns.Cfg.Smtp.FromName, ns.Cfg.Smtp.FromAddress),
		Subject:      subject,
		Body:         buffer.String(),
		EmbededFiles: cmd.EmbededFiles,
	}, nil
}
