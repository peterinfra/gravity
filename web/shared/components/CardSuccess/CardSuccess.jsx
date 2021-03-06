/*
Copyright 2019 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

import React from 'react';
import { Card, Text } from 'shared/components';
import * as Icons from 'shared/components/Icon';

export default function CardSuccess({ title, children }) {
  return (
    <Card width="540px" p={7} my={4} mx="auto" textAlign="center">
      <Icons.CircleCheck mb={3} fontSize={64} color="success"/>
      <Text typography="h1" mb="3">
        {title}
      </Text>
      <Text typography="paragraph">
        {children}
      </Text>
    </Card>
  )
}

export function CardSuccessLogin() {
  return (
    <CardSuccess title="Login Successful">
      You have successfully signed into your account.
      You can close this window and continue using the product.
    </CardSuccess>
  )
}