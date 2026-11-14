# revision_and_reads.feature — wave 2: the revise and retire commands, the
# read side (list/get contents, list categories) with its paging rules, the
# category move and the content/category validation matrix.
Feature: Revision, retirement and the read side

  Background:
    Given a category "Bottles" exists
    And offer-availability reports item "sku-1" created

  Scenario: Published content is revised and published again
    When the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-1","description":"A steel bottle.","bullets":["Double-wall"],
       "media":[{"mediaId":"m1","url":"https://static.example.invalid/a.jpg","altText":"side","role":"PRIMARY"}],
       "primaryCategoryId":"{category:Bottles}"}
      """
    Then the API answers 201
    When the API posts to "/product-contents/sku-1/publications" with Idempotency-Key "p1"
    Then the API answers 200
    And the API response field "state" is "PUBLISHED"
    When the API posts to "/product-contents/sku-1/revisions" with Idempotency-Key "r1" the body:
      """
      {"catalogItemId":"sku-1","description":"A better steel bottle.","bullets":["Double-wall","BPA-free"],
       "media":[{"mediaId":"m1","url":"https://static.example.invalid/a.jpg","altText":"side","role":"PRIMARY"}],
       "primaryCategoryId":"{category:Bottles}"}
      """
    Then the API answers 200
    And the API response field "state" is "PUBLISHED"
    And the API response field "revision" is "2"
    And the API response field "publishedRevision" is "1"
    When the API posts to "/product-contents/sku-1/publications" with Idempotency-Key "p2"
    Then the API answers 200
    And the API response field "state" is "PUBLISHED"
    And the API response field "publishedRevision" is "2"
    And 2 "ProductContentPublished" events were published

  Scenario: Retired content never changes again
    When the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-1","description":"A steel bottle.","bullets":["Double-wall"],
       "media":[{"mediaId":"m1","url":"https://static.example.invalid/a.jpg","altText":"side","role":"PRIMARY"}],
       "primaryCategoryId":"{category:Bottles}"}
      """
    And the API posts to "/product-contents/sku-1/publications" with Idempotency-Key "p1"
    And the API posts to "/product-contents/sku-1/retirements" with Idempotency-Key "t1"
    Then the API answers 200
    And the API response field "state" is "RETIRED"
    And 1 "ProductContentRetired" events were published
    When the API posts to "/product-contents/sku-1/revisions" with Idempotency-Key "r1" the body:
      """
      {"catalogItemId":"sku-1","description":"Too late.","bullets":["x"],
       "media":[{"mediaId":"m1","url":"https://static.example.invalid/a.jpg","altText":"side","role":"PRIMARY"}],
       "primaryCategoryId":"{category:Bottles}"}
      """
    Then the API answers 409 with problem "content-retired"

  Scenario: Content is read back and listed with the paging rules
    Given offer-availability reports item "sku-2" created
    When the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-1","description":"A steel bottle."}
      """
    And the API posts to "/product-contents" with Idempotency-Key "c2" the body:
      """
      {"catalogItemId":"sku-2","description":"A glass jar."}
      """
    Then the API answers 201
    When the API requests "/product-contents"
    Then the API answers 200
    And the API response list "items" has 2 items
    When the API requests "/product-contents?limit=1"
    Then the API answers 200
    And the API response list "items" has 1 items
    When the API requests "/product-contents/sku-1"
    Then the API answers 200
    And the API response field "description" is "A steel bottle."
    When the API requests "/product-contents/sku-404"
    Then the API answers 404 with problem "not-found"
    When the API requests "/product-contents?limit=0"
    Then the API answers 400 with problem "invalid-limit"
    When the API requests "/product-contents?cursor=not-a-cursor"
    Then the API answers 400 with problem "invalid-cursor"
    When the API requests "/product-contents?bogus=1"
    Then the API answers 400 with problem "unknown-query-parameter"

  Scenario: Categories are listed
    Given a category "Jars" exists
    When the API requests "/categories"
    Then the API answers 200
    And the API response list "categories" has 2 items

  Scenario: A category moves, never under its own subtree
    Given a category "Drinkware" exists
    And a category "Steel" exists under "Drinkware"
    When the API puts to "/categories/{category:Drinkware}/parent" the body:
      """
      {"parentId":"{category:Steel}"}
      """
    Then the API answers 422 with problem "category-cycle"
    When the API puts to "/categories/{category:Steel}/parent" the body:
      """
      {"parentId":null}
      """
    Then the API answers 200

  Scenario: A category never moves under a parent the tree does not hold
    When the API puts to "/categories/{category:Bottles}/parent" the body:
      """
      {"parentId":"cat-404"}
      """
    Then the API answers 422 with problem "unknown-category"

  Scenario: Content limits are validated
    When the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-1","description":"Too much.","bullets":["1","2","3","4","5","6","7","8","9","10","11"]}
      """
    Then the API answers 422 with problem "too-many-bullets"
    When the API posts to "/product-contents" with Idempotency-Key "c1b" the body:
      """
      {"catalogItemId":"sku-1","description":"At the cap.","bullets":["1","2","3","4","5","6","7","8","9","10"]}
      """
    Then the API answers 201
    And the API response list "bullets" has 10 items
    When the API deletes "/product-contents/sku-1/draft"
    Then the API answers 200
    When the API posts to "/product-contents" with Idempotency-Key "c2" the body:
      """
      {"catalogItemId":"sku-1","description":"Too much.",
       "media":[{"mediaId":"m1","url":"https://static.example.invalid/1.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m2","url":"https://static.example.invalid/2.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m3","url":"https://static.example.invalid/3.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m4","url":"https://static.example.invalid/4.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m5","url":"https://static.example.invalid/5.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m6","url":"https://static.example.invalid/6.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m7","url":"https://static.example.invalid/7.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m8","url":"https://static.example.invalid/8.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m9","url":"https://static.example.invalid/9.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m10","url":"https://static.example.invalid/10.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m11","url":"https://static.example.invalid/11.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m12","url":"https://static.example.invalid/12.jpg","altText":"a","role":"GALLERY"},
                {"mediaId":"m13","url":"https://static.example.invalid/13.jpg","altText":"a","role":"GALLERY"}]}
      """
    Then the API answers 422 with problem "too-many-media"

  Scenario: Content of a discontinued item is refused
    Given offer-availability reports item "sku-9" created
    And offer-availability reports item "sku-9" discontinued
    When the API posts to "/product-contents" with Idempotency-Key "c9" the body:
      """
      {"catalogItemId":"sku-9","description":"Gone."}
      """
    Then the API answers 422 with problem "item-discontinued"

  Scenario: The platform endpoints answer
    When the API requests "/healthz"
    Then the API answers 200
    When the API requests "/readyz"
    Then the API answers 200
