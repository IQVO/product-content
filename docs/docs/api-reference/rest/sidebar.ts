import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
  apisidebar: [
    {
      type: "doc",
      id: "api-reference/rest/product-content-api",
    },
    {
      type: "category",
      label: "ProductContents",
      link: {
        type: "doc",
        id: "api-reference/rest/product-contents",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/create-product-content",
          label: "Create the draft content of a catalogue item",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/list-product-contents",
          label: "List product contents, paged (projection bootstrap)",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest/get-product-content",
          label: "Get the content of one catalogue item",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest/revise-product-content",
          label: "Revise the content of a catalogue item",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/publish-product-content",
          label: "Publish the content of a catalogue item",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/retire-product-content",
          label: "Retire the published content of a catalogue item",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/discard-product-content-draft",
          label: "Discard the draft content of a catalogue item",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "Categories",
      link: {
        type: "doc",
        id: "api-reference/rest/categories",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/create-category",
          label: "Create a category in the live tree",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/list-categories",
          label: "Get the category tree",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest/publish-category-tree",
          label: "Publish the category tree again",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/move-category",
          label: "Move a category under a new parent",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/rename-category",
          label: "Rename a category",
          className: "api-method post",
        },
      ],
    },
  ],
};

export default sidebar.apisidebar;
