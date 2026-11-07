<?xml version="1.0" encoding="UTF-8"?>
<!-- Reads an order of the ordering system into the canonical format of /TRFIN/ORDER:
     <order id="O-1" company="C1" date="2026-11-07"><amount>12.50</amount><note>…</note></order>
     Without note the note is null; an empty note is the empty string. -->
<xsl:stylesheet version="3.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="xml" omit-xml-declaration="yes"/>
  <xsl:template match="/order">
    <value>
      <ORDER_ID><xsl:value-of select="@id"/></ORDER_ID>
      <COMPANY_ID><xsl:value-of select="@company"/></COMPANY_ID>
      <ORDERED_ON><xsl:value-of select="@date"/></ORDERED_ON>
      <AMOUNT><xsl:value-of select="format-number(number(amount), '0.00')"/></AMOUNT>
      <xsl:if test="note">
        <NOTE><xsl:value-of select="note"/></NOTE>
      </xsl:if>
    </value>
  </xsl:template>
</xsl:stylesheet>
