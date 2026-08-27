import { expect, test } from '@playwright/test';

test('shows the Market Lens foundation shell', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Market Lens' })).toBeVisible();
  await expect(page.getByText('Stock research and strategy experimentation platform')).toBeVisible();
  await expect(page.getByText('Foundation stage')).toBeVisible();
});
