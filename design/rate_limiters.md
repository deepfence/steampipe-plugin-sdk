
# Rate Limiting

## Overview

Rate limiting can be applied to all `Get`, `List` and column `Hydrate` calls.

For each call, multiple rate limiters may apply. When more than one limiter applies to a call, 
the rate limiter with the largest required wait is respected.

The rate limiters which apply to a call are resolved using `scopes`. Each rate limiter definition specifies
scopes which apply to it, for example `region`, `connection`. Then for each call, the values of these scopes are 
determined and used to identify which limiters apply. 


## Defining Rate Limiters

Rate limiters may be defined in the plugin definition (by the plugin author), or in HCL config (by the user) 

### Plugin Definition
A rate limiters is defined using the `Definition` struct:
```go
type Definition struct {
	// the limiter name
	Name string
	// the actual limiter config
	FillRate   rate.Limit
	BucketSize int

	// the scopes which identify this limiter instance
	// one limiter instance will be created for each combination of scopes which is encountered
	Scopes []string

	// filter used to target the limiter
	Where        string
}
```
`Scopes` is list of all the scopes which apply to the rate limiter. 
For example, if you want a rate limiter that applies to a single account, region and service, you could use the scopes:
[`connection`, `region`,`service`].
(See below for details of predefined vs custom scope names)

`Where` is a SQL compatible where clause which allows a rate limiter to be targeted to spcific set of scope values, 
for example to specify a rate limiter for a specific service only, the filter `"service"="s3` be used. 

For example:
```go
p := &plugin.Plugin{
		Name: "aws",
		TableMap: map[string]*plugin.Table{...}
		RateLimiters: []*rate_limiter.Definition{
			Name:       "connection-region-service",
			BucketSize: 10,
			FillRate:   50,
			Scopes:     []string{"region", "connection", "servive"},
			Where:      "service = 's3'",
			},
		},
```

### HCL Definition
Rate limiters may be define in HCL in an `.spc` file in the config folder. 
If a limiter has the same name as one defined in the plugin it will override it, if not, a new limiter is defined.

```
limiter "connection-region-service" {
  plugin       = "aws"  
  bucket_size  = 5
  fill_rate    = 25
  scope  = ["region", "connection", "servive"]
  where  = "service = 's3'"
}

```

## Resolving Rate Limiters

When executing a hydrate call the following steps are followed:
1) Build the set of rate limiter definitions which may apply to the hydrate call
2) Build the set of scope values which apply to the hydrate call
3) Determine which limiter defintions are satisfied by the scope values (looking at both required scopes and the scope filters)
4) Build a MultiLimiter from the resultant limiter defintions

### Resolving Scope Values
Scope values are popuylated from 3 sources:
- *implicit* scope values populated automatically 
  - `tabe`, `connection`
- *matrix* scope values populated from matrix quals (e.g. `region`)
- *custom* scope values (tags?) which may be defined in `Table` defintions, `HydrateConfig`, `GetConfig` and  `ListConfig`   

## Paged List Calls

If the list call uses paging, the SDK provides a hook, `WaitForListRateLimit`, which can be called before paging to apply rate limiting to the list call:

```go
	// List call
	for paginator.HasMorePages() {
		
		// apply rate limiting
		d.WaitForListRateLimit(ctx)
		
		output, err := paginator.NextPage(ctx)
		if err != nil {
			plugin.Logger(ctx).Error("aws_codepipeline_pipeline.listCodepipelinePipelines", "api_error", err)
			return nil, err
		}
		for _, items := range output.Pipelines {
			d.StreamListItem(ctx, items)

			// Context can be cancelled due to manual cancellation or the limit has been hit
			if d.RowsRemaining(ctx) == 0 {
				return nil, nil
			}
		}
	}
```

## Scenarios

### 1. Plugin defines a single unscoped rate limiter

```go
func Plugin(_ context.Context) *plugin.Plugin {
	p := &plugin.Plugin{
		Name:     "aws",
		TableMap: map[string]*plugin.Table{...},
		RateLimiters: []*rate_limiter.Definition{
            {
                Limit:     50,
                BurstSize: 10,
            },
        },
		...
	}

	return p
}
```

### 2. Plugin defines a rate limiter scoped by implicit scope "connection", custom scope "service" and matrix scope "region"  

#### Plugin definition
```go

func Plugin(_ context.Context) *plugin.Plugin {
	p := &plugin.Plugin{
		Name:     pluginName,
		TableMap: map[string]*plugin.Table{...},
		RateLimiters:[]*rate_limiter.Definition{
            {
                Limit:     50,
                BurstSize: 10,
                Scopes: []string{
                        "connection",
                        "service"
                        "region",
                },
            },
        },
		...
	}

	return p
}
```
NOTE: `region` must be defined as a matrix qual in order to use the matrix scope value, 
and `service` must be defined as a custom scope value for tables or hydrate calls which this limiter targets.    

#### 2a. Table definition which defines a "region" key column and sets the "service" scope value for all hydrate calls

```go
func tableAwsS3AccessPoint(_ context.Context) *plugin.Table {
	return &plugin.Table{
        Name: "aws_s3_access_point",
		List: &plugin.ListConfig{
			Hydrate:    listS3AccessPoints,
			KeyColumns: plugin.SingleColumn("region"),
		},
		Get: &plugin.GetConfig{
			KeyColumns: plugin.AllColumns([]string{"name", "region"}),
			Hydrate:    getS3AccessPoint,
		},
		// set "service" scope to "s3" for all hydrate calls
        Tags: map[string]string{
            "service": "s3",
        },
		Columns: awsRegionalColumns([]*plugin.Column{...}),
	}
}

```
#### 2b. Hydrate call definition which specifies the "service" scope value


```go
func tableAwsS3AccountSettings(_ context.Context) *plugin.Table {
	return &plugin.Table{
		Name: "aws_s3_account_settings",
		List: &plugin.ListConfig{...},
		HydrateConfig: []plugin.HydrateConfig{
			{
				Func: getAccountBucketPublicAccessBlock,
                // set the "service" scope value for this hydrate call
                Tags: map[string]string{
                    "service": "s3",
				},
			},
		},
		Columns: awsGlobalRegionColumns([]*plugin.Column{...}),
	}
}

```


### 3. Plugin defines rate limiters for "s3" and "ec2" services and one for all other services
NOTE: also scoped by "connection" and "region"

```go

// scopes used for all rate limiters
var rateLimiterScopes=[]string{"connection","service","region",}

func Plugin(_ context.Context) *plugin.Plugin {
	p := &plugin.Plugin{
		Name: pluginName,
		TableMap: map[string]*plugin.Table{ ...	},
		RateLimiters: []*rate_limiter.Definition{
            // rate limiter for s3 service
            {
                Limit:     20,
                BurstSize: 5,
                Scopes: rateLimiterScopes,
                Where: "service='s3'",
                },
            },
            // rate limiter for ec2 service
            {
                Limit:     40,
                BurstSize: 5,
                Scopes: rateLimiterScopes,
                Where: "service='ec2'",
            },
            // rate limiter for all other services
            {
                Limit:     75,
                BurstSize: 10,
                Where: "service not in ('s3,'ec2')",
            },
        },
		...
	}

	return p
}
```
