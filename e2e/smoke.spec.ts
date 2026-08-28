import { expect, test } from '@playwright/test';

test('shows the Market Lens foundation shell', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Market Lens' })).toBeVisible();
  await expect(page.getByText('Stock research and strategy experimentation platform')).toBeVisible();
  await expect(page.getByText('Foundation stage')).toBeVisible();
});

test('keeps the foundation shell within a 320px viewport', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/');

  const hasHorizontalOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(hasHorizontalOverflow).toBe(false);
  await expect(page.getByRole('heading', { name: 'Market Lens' })).toBeVisible();
});
