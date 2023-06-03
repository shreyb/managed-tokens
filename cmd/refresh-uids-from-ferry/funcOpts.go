package main

// TODO:  Get rid of this file

// Functional options for use in setting up kerberos/JWT Auth
// TODO:  Move these into worker.Config file so we define the functional options there?

// setkrb5ccname sets the KRB5CCNAME directory environment variable in the worker.Config's
// environment
// func setkrb5ccname(krb5ccname string) func(c *worker.Config) error {
// 	return func(c *worker.Config) error {
// 		c.CommandEnvironment.Krb5ccname = "KRB5CCNAME=DIR:" + krb5ccname
// 		return nil
// 	}
// }

// // setKeytabPath sets the location of a worker.Config's kerberos keytab
// func setKeytabPath() func(c *worker.Config) error {
// 	return func(c *worker.Config) error {
// 		c.KeytabPath = viper.GetString("ferry.serviceKeytabPath")
// 		return nil
// 	}
// }

// // setUserPrincipalAndHtgettokenopts sets a worker.Config's kerberos principal and with it, the HTGETTOKENOPTS environment variable
// func getUserPrincipalAndHtgettokenopts() (string, string) {
// 	var htgettokenOpts string
// 	userPrincipal := viper.GetString("ferry.serviceKerberosPrincipal")
// 	credKey := strings.ReplaceAll(userPrincipal, "@FNAL.GOV", "")

// 	if viper.IsSet("htgettokenopts") {
// 		htgettokenOpts = viper.GetString("htgettokenopts")
// 	} else {
// 		htgettokenOpts = "--credkey=" + credKey
// 	}
// 	return userPrincipal, htgettokenOpts
// }
