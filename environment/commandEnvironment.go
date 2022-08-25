package environment

type CommandEnvironment struct {
	Krb5ccname          string
	CondorCreddHost     string
	CondorCollectorHost string
	HtgettokenOpts      string
}

func (c *CommandEnvironment) ToMap() map[string]string {
	return map[string]string{
		"Krb5ccname":          c.Krb5ccname,
		"CondorCreddHost":     c.CondorCreddHost,
		"CondorCollectorHost": c.CondorCollectorHost,
		"HtgettokenOpts":      c.HtgettokenOpts,
	}
}

func (c *CommandEnvironment) ToEnvs() map[string]string {
	return map[string]string{
		"Krb5ccname":          "KRB5CCNAME",
		"CondorCreddHost":     "_condor_CREDD_HOST",
		"CondorCollectorHost": "_condor_COLLECTOR_HOST",
		"HtgettokenOpts":      "HTGETTOKENOPTS",
	}
}

type EnvironmentMapper interface {
	ToMap() map[string]string
	ToEnvs() map[string]string
}
