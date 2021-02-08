// Copyright (c) 2021 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils_test

import (
	. "github.com/gardener/gardener/pkg/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/utils/pointer"
)

type object struct {
	Object  *object  `json:"object,omitempty"`
	Objects []object `json:"objects,omitempty"`
	String  *string  `json:"string,omitempty"`
	Int     *int32   `json:"int,omitempty"`
	Bool    *bool    `json:"bool,omitempty"`
}

var _ = Describe("Values", func() {
	var (
		obj    *object
		values map[string]interface{}
	)

	BeforeEach(func() {
		obj = &object{
			Objects: []object{
				{
					Object: &object{
						String: pointer.StringPtr("foo"),
					},
					Int: pointer.Int32Ptr(42),
				},
			},
			Bool: pointer.BoolPtr(true),
		}
		values = map[string]interface{}{
			"objects": []interface{}{
				map[string]interface{}{
					"object": map[string]interface{}{
						"string": "foo",
					},
					"int": float64(42),
				},
			},
			"bool": true,
		}
	})

	Describe("#ToValuesMap", func() {
		It("should convert an object to a values map", func() {
			result, err := ToValuesMap(obj)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should convert an empty object to an empty values map", func() {
			result, err := ToValuesMap(&object{})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(map[string]interface{}{}))
		})

		It("should convert nil to a nil values map", func() {
			result, err := ToValuesMap(nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should fail if the object cannot be marshalled to JSON", func() {
			_, err := ToValuesMap(func() {})
			Expect(err).To(HaveOccurred())
		})

		It("should fail if the object cannot be unmarshalled back to a values map", func() {
			_, err := ToValuesMap("foo")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("#FromValuesMap", func() {
		var result *object

		BeforeEach(func() {
			result = nil
		})

		It("should convert a values map to an object", func() {
			err := FromValuesMap(values, &result)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(obj))
		})

		It("should convert an empty values map to an empty object", func() {
			err := FromValuesMap(map[string]interface{}{}, &result)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(&object{}))
		})

		It("should convert a nil values map to nil", func() {
			err := FromValuesMap(nil, &result)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should fail if the values map cannot be marshalled to JSON", func() {
			err := FromValuesMap(map[string]interface{}{"foo": func() {}}, &result)
			Expect(err).To(HaveOccurred())
		})

		It("should fail if the values map cannot be unmarshalled back to an object", func() {
			err := FromValuesMap(map[string]interface{}{"object": "foo"}, &result)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("#InitValuesMap", func() {
		It("should return the given values map if it is not nil", func() {
			Expect(InitValuesMap(values)).To(Equal(values))
		})

		It("should return a new values map if the given values map is nil", func() {
			Expect(InitValuesMap(nil)).To(Equal(map[string]interface{}{}))
		})
	})

	Describe("#GetFromValuesMap", func() {
		It("should return the element at the specified location in the given values map", func() {
			result, err := GetFromValuesMap(values, "objects", 0, "object")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(map[string]interface{}{"string": "foo"}))
		})

		It("should return nil if a map key doesn't exist", func() {
			result, err := GetFromValuesMap(values, "foo", "bar")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should return nil if a slice index doesn't exist", func() {
			result, err := GetFromValuesMap(values, "objects", 1, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should return nil with a nil values map", func() {
			result, err := GetFromValuesMap(nil, "foo")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should return the given values map with no keys", func() {
			result, err := GetFromValuesMap(values)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should fail if a string key is specified but its element is not a map", func() {
			result, err := GetFromValuesMap(values, "objects", "foo")
			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should fail if an int key is specified but its element is not a slice", func() {
			result, err := GetFromValuesMap(values, 0)
			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should fail if a key is of type neither string nor int", func() {
			result, err := GetFromValuesMap(values, true)
			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
		})
	})

	Describe("#SetToValuesMap", func() {
		It("should set the element at the specified location in the given values map", func() {
			result, err := SetToValuesMap(values, map[string]interface{}{"foo": "bar"}, "objects", 0, "object")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(map[string]interface{}{
				"objects": []interface{}{
					map[string]interface{}{
						"object": map[string]interface{}{
							"foo": "bar",
						},
						"int": float64(42),
					},
				},
				"bool": true,
			}))
		})

		It("should create the element if a map key doesn't exist", func() {
			result, err := SetToValuesMap(values, map[string]interface{}{"foo": "bar"}, "foo", "bar")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(map[string]interface{}{
				"objects": []interface{}{
					map[string]interface{}{
						"object": map[string]interface{}{
							"string": "foo",
						},
						"int": float64(42),
					},
				},
				"bool": true,
				"foo": map[string]interface{}{
					"bar": map[string]interface{}{
						"foo": "bar",
					},
				},
			}))
		})

		It("should create the element if a slice index doesn't exist", func() {
			result, err := SetToValuesMap(values, map[string]interface{}{"foo": "bar"}, "objects", 1, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(map[string]interface{}{
				"objects": []interface{}{
					map[string]interface{}{
						"object": map[string]interface{}{
							"string": "foo",
						},
						"int": float64(42),
					},
					[]interface{}{
						map[string]interface{}{
							"foo": "bar",
						},
					},
				},
				"bool": true,
			}))
		})

		It("should create a new values map with a nil values map", func() {
			result, err := SetToValuesMap(nil, map[string]interface{}{"foo": "bar"}, "foo")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(map[string]interface{}{
				"foo": map[string]interface{}{
					"foo": "bar",
				},
			}))
		})

		It("should return the given values map with no keys", func() {
			result, err := SetToValuesMap(values, map[string]interface{}{"foo": "bar"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should return nil with a nil values map and no keys", func() {
			result, err := SetToValuesMap(nil, map[string]interface{}{"foo": "bar"})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should fail if a string key is specified but its element is not a map", func() {
			result, err := SetToValuesMap(values, nil, "objects", "foo")
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should fail if an int key is specified but its element is not a slice", func() {
			result, err := SetToValuesMap(values, nil, 0)
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should fail if a key is of type neither string nor int", func() {
			result, err := SetToValuesMap(values, nil, true)
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should fail if an index is out of range", func() {
			result, err := SetToValuesMap(values, nil, "objects", 2, "object")
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(values))
		})
	})

	Describe("#DeleteFromValuesMap", func() {
		It("should delete the element at the specified location in the given values map", func() {
			result, err := DeleteFromValuesMap(values, "objects", 0, "object")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(map[string]interface{}{
				"objects": []interface{}{
					map[string]interface{}{
						"int": float64(42),
					},
				},
				"bool": true,
			}))
		})

		It("should return the given values map if a map key doesn't exist", func() {
			result, err := DeleteFromValuesMap(values, "foo", "bar")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should return the given values map if a slice index doesn't exist", func() {
			result, err := DeleteFromValuesMap(values, "objects", 1, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should return nil with a nil values map", func() {
			result, err := DeleteFromValuesMap(nil, "foo")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should return the given values map with no keys", func() {
			result, err := DeleteFromValuesMap(values)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should fail if a string key is specified but its element is not a map", func() {
			result, err := DeleteFromValuesMap(values, "objects", "foo")
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should fail if an int key is specified but its element is not a slice", func() {
			result, err := DeleteFromValuesMap(values, 0)
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(values))
		})

		It("should fail if a key is of type neither string nor int", func() {
			result, err := DeleteFromValuesMap(values, true)
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(values))
		})
	})
})
