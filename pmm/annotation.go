/*
	Copyright (c) 2016, Percona LLC and/or its affiliates. All rights reserved.

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU Affero General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU Affero General Public License for more details.

	You should have received a copy of the GNU Affero General Public License
	along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package pmm

import (
	"context"
	"errors"
	"regexp"

	"github.com/percona/pmm-client/pmm/managed"
)

// AddAnnotation posts annotation to managed.
func (a *Admin) AddAnnotation(ctx context.Context, text string, tags string) error {
	if text == "" {
		return errors.New("failed to save annotation (empty annotation is not allowed)")
	}
	return a.managedAPI.AnnotationCreate(ctx, &managed.APIAnnotationCreateRequest{
		Text: text,
		// split by comma and trim spaces if they were after comma
		Tags: regexp.MustCompile(`,\s*`).Split(tags, -1),
	})
}
